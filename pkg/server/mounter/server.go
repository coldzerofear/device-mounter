package mounter

import (
	"context"
	"fmt"
	"strings"

	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/client"
	"github.com/coldzerofear/device-mounter/pkg/framework"
	"github.com/coldzerofear/device-mounter/pkg/util"
	"github.com/opencontainers/runc/libcontainer/configs"
	v1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

func NewDeviceMounterServer(
	nodeName string, kubeClient *kubernetes.Clientset,
	podLister listerv1.PodLister, nodeLister listerv1.NodeLister,
	recorder record.EventRecorder) *DeviceMounterServer {
	return &DeviceMounterServer{
		nodeName:   nodeName,
		kubeClient: kubeClient,
		recorder:   recorder,
		nodeLister: nodeLister,
		podLister:  podLister,
	}
}

type DeviceMounterServer struct {
	api.UnimplementedDeviceMountServiceServer
	nodeName   string
	kubeClient *kubernetes.Clientset
	recorder   record.EventRecorder
	nodeLister listerv1.NodeLister
	podLister  listerv1.PodLister
}

func (s *DeviceMounterServer) MountDevice(ctx context.Context, req *api.MountDeviceRequest) (resp *api.DeviceResponse, err error) {
	klog.V(4).Infoln("MountDevice Called", "Request", req)

	defer func() {
		if err != nil && resp == nil {
			mErr, ok := err.(*api.MounterError)
			if ok {
				resp = &api.DeviceResponse{
					Result:  mErr.Code,
					Message: mErr.Message,
				}
			} else {
				resp = &api.DeviceResponse{
					Result:  api.ResultCode_Fail,
					Message: err.Error(),
				}
			}
		}
		if resp != nil {
			err = nil
		}
	}()

	if err = CheckMountDeviceRequest(req); err != nil {
		klog.V(4).Infoln(err.Error())
		return
	}

	var pod *v1.Pod
	pod, err = s.GetTargetPod(ctx, req.PodName, req.PodNamespace)
	if apierror.IsNotFound(err) {
		klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
		resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		return
	} else if err != nil {
		klog.ErrorS(err, "Get target Pod failed", "name", req.PodName, "namespace", req.PodNamespace)
		return
	}

	// Verify container
	var container *api.Container
	if container, err = CheckPodContainer(pod, req.GetContainer()); err != nil {
		return
	}
	if err = CheckPodContainerStatus(pod, container); err != nil {
		return
	}

	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.GetDeviceMounter(deviceType)
	if !ok {
		err = fmt.Errorf("Unsupported device type: %s", req.GetDeviceType())
		return
	}

	// Resource name format conversion.
	resources := make(map[v1.ResourceName]resource.Quantity)
	var quantity resource.Quantity
	for key, val := range req.GetResources() {
		quantity, err = resource.ParseQuantity(val)
		if err != nil {
			return
		}
		resources[v1.ResourceName(key)] = quantity
	}

	// get current node
	node, err := s.nodeLister.Get(s.nodeName)
	if apierror.IsNotFound(err) {
		klog.ErrorS(err, "Not found target node", "node", s.nodeName)
		resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		return
	} else if err != nil {
		klog.ErrorS(err, "Get target node failed", "node", s.nodeName)
		return
	}

	node = node.DeepCopy()
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)

	err = deviceMounter.ValidateMountRequest(ctx, kubeClient, node, pod,
		container, resources, req.GetAnnotations(), req.GetLabels())
	if err != nil {
		klog.V(3).ErrorS(err, "validate mount request failed")
		return
	}

	var slavePods []*v1.Pod
	slavePods, err = s.GetSlavePods(deviceType, pod, container)
	if apierror.IsNotFound(err) {
		klog.ErrorS(err, "Not found slave pods")
		resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		return
	} else if err != nil {
		klog.ErrorS(err, "Get slave pods failed")
		return
	}

	// Build a list of pod templates to be created.
	slavePods, err = deviceMounter.BuildSupportPodTemplates(ctx, pod, container, resources, req.GetAnnotations(), req.GetLabels(), slavePods)
	if err != nil {
		klog.V(3).ErrorS(err, "Build device slave pods failed")
		return
	}

	for i, slavePod := range slavePods {
		targetPod := slavePod.DeepCopy()
		targetPod, err = s.PatchPod(targetPod, req.GetPatches())
		if err != nil {
			return
		}
		s.MutationPodFunc(deviceType, container, pod, targetPod)
		slavePods[i] = targetPod
	}

	var (
		slavePodKeys  []api.ObjectKey
		rollbackRules func() error // 回滚设备规则方法
		closedFd      func() error // 关闭文件句柄方法
		rollbackFiles func() error // 回滚设备文件方法
	)
	// Create built slave pods.
	for _, slavePod := range slavePods {
		slavePod, err = s.kubeClient.CoreV1().Pods(slavePod.Namespace).Create(ctx, slavePod, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Create Slave Pod failed: %v", err)
			break
		}
		slavePodKey := api.ObjectKeyFromObject(slavePod)
		slavePodKeys = append(slavePodKeys, slavePodKey)
		_, pdbErr := s.CreatePodDisruptionBudget(ctx, slavePod)
		if pdbErr != nil {
			klog.V(4).ErrorS(pdbErr, fmt.Sprintf("Create Slave Pod %s PDB failed", slavePodKey.String()))
		}
	}

	// When an error occurs during the execution of steps,
	// roll back the operation in the specified order to ensure atomicity.
	defer func() {
		if err != nil {
			if rollbackFiles != nil {
				rErr := rollbackFiles()
				klog.V(4).Infof("Roll back device files: %v", rErr)
			}
			if rollbackRules != nil {
				rErr := rollbackRules()
				klog.V(4).Infof("Roll back device rules: %v", rErr)
			}
			// delete the created slave pods, prevent resource leakage.
			_ = GarbageCollectionPods(s.kubeClient, slavePodKeys)
		}
		if closedFd != nil {
			_ = closedFd()
		}
	}()

	if err != nil || len(slavePodKeys) == 0 {
		err = fmt.Errorf("Device slave pod creation failed: %v", err)
		return
	}
	klog.Infoln("Successfully created slave pods", slavePodKeys)
	var (
		readyPods []*v1.Pod
		skipPods  []*v1.Pod
	)
	// Waiting for the created pods to be ready.
	readyPods, skipPods, err = WaitSupportPodsReady(ctx, s.podLister, s.kubeClient, deviceMounter, slavePodKeys)
	if err != nil {
		klog.V(4).ErrorS(err, "Wait slave pods ready failed")
		return
	}
	// 如果此时没有ready的pod则返回错误
	if len(readyPods) == 0 {
		err = fmt.Errorf("waiting for device pod to be ready failed")
		return
	}

	// Get device mounting list information.
	var deviceInfos []api.DeviceInfo
	deviceInfos, err = deviceMounter.GetDeviceInfosToMount(ctx, kubeClient, pod, container, readyPods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get mount device info error")
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("failed to detect mount device info: %v", err)
		}
		return
	}

	var (
		pids       []int
		cgroupPath string
	)
	pids, cgroupPath, err = s.GetContainerCGroupPathAndPids(pod, container)
	if err != nil {
		return
	}

	klog.V(4).Infoln("current container pids", pids)

	res := &configs.Resources{SkipDevices: false}
	for i, dev := range deviceInfos {
		klog.V(3).Infoln("Device Rule", "Index", i, "DeviceFile", dev.DeviceFilePath, "Type",
			dev.Type, "Major", dev.Major, "Minor", dev.Minor, "Permissions", dev.Permissions, "Allow", dev.Allow)
		res.Devices = append(res.Devices, &deviceInfos[i].Rule)
	}

	closedFd, rollbackRules, err = s.DeviceRuleSetFunc(cgroupPath, res)
	if err != nil {
		klog.V(4).ErrorS(err, "set cgroup device permissions error")
		err = fmt.Errorf("failed to set access permissions for cgroup devices: %v", err)
		return
	}

	config := &util.Config{Target: pids[0], Mount: true}
	rollbackFiles, err = s.CreateDeviceFiles(config, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Create Device Files error")
		err = fmt.Errorf("failed to create devic files: %v", err)
		return
	}

	err = deviceMounter.ExecutePostMountActions(ctx, kubeClient, *config, pod, container, readyPods)
	if err != nil {
		klog.Warningf("execute post mount actions error: %v", err)
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("failed to execute post mount actions: %v", err)
		}
		return
	}

	// Delete the previously skipped pod list.
	skipPodKeys := make([]api.ObjectKey, len(skipPods))
	for i, skipPod := range skipPods {
		skipPodKeys[i] = api.ObjectKeyFromObject(skipPod)
	}
	_ = GarbageCollectionPods(s.kubeClient, skipPodKeys)

	message := fmt.Sprintf("Successfully mounted %s devices", deviceType)
	s.recorder.Event(pod, v1.EventTypeNormal, "MountDevice", message)
	klog.Infoln(deviceType, "MountDevice Successfully")
	resp = &api.DeviceResponse{Result: api.ResultCode_Success, Message: message}
	return
}

func (s *DeviceMounterServer) UnMountDevice(ctx context.Context, req *api.UnMountDeviceRequest) (resp *api.DeviceResponse, err error) {
	klog.V(4).Infoln("UnMountDevice Called", "Request", req)

	defer func() {
		if err != nil && resp == nil {
			mErr, ok := err.(*api.MounterError)
			if ok {
				resp = &api.DeviceResponse{
					Result:  mErr.Code,
					Message: mErr.Message,
				}
			} else {
				resp = &api.DeviceResponse{
					Result:  api.ResultCode_Fail,
					Message: err.Error(),
				}
			}
		}
		if resp != nil {
			err = nil
		}
	}()

	if err = CheckUnMountDeviceRequest(req); err != nil {
		klog.V(4).Infoln(err.Error())
		return
	}

	var pod *v1.Pod
	pod, err = s.GetTargetPod(ctx, req.PodName, req.PodNamespace)
	if apierror.IsNotFound(err) {
		klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
		resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		return
	} else if err != nil {
		klog.ErrorS(err, "Get target Pod failed", "name", req.PodName, "namespace", req.PodNamespace)
		return
	}

	// Verify container
	var container *api.Container
	if container, err = CheckPodContainer(pod, req.GetContainer()); err != nil {
		return
	}
	if err = CheckPodContainerStatus(pod, container); err != nil {
		return
	}

	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.GetDeviceMounter(deviceType)
	if !ok {
		err = fmt.Errorf("Unsupported device type: %s", req.GetDeviceType())
		return
	}

	// Query the slave pods to which the current container belongs.
	slavePods, err := s.GetSlavePods(deviceType, pod, container)
	if apierror.IsNotFound(err) {
		klog.ErrorS(err, "Not found slave pods")
		resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		return
	} else if err != nil {
		klog.ErrorS(err, "Get slave pods failed")
		return
	}

	if len(slavePods) == 0 {
		msg := fmt.Sprintf("No device found for uninstallation")
		err = api.NewMounterError(api.ResultCode_NotFound, msg)
		return
	}
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)

	// Retrieve the list of device information to be uninstalled.
	deviceInfos, err := deviceMounter.GetDeviceInfosToUnmount(ctx, kubeClient, pod, container, slavePods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get unmount device info error")
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("Failed to detect uninstalled device info: %v", err)
		}
		return
	}

	var (
		pids       []int
		cgroupPath string
	)
	pids, cgroupPath, err = s.GetContainerCGroupPathAndPids(pod, container)
	if err != nil {
		return
	}

	klog.V(4).Infoln("current container pids", pids)

	config := &util.Config{Target: pids[0], Mount: true}
	processes, err := deviceMounter.GetDevicesActiveProcessIDs(ctx, pids, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Get device running processes error")
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("Failed to retrieve the list of device container processes: %v", err)
		}
		return
	}
	// When the request does not define forced uninstallation and there are running processes on the device,
	// an error is returned stating that the device is busy.
	if !req.GetForce() && len(processes) > 0 {
		msg := fmt.Sprintf("The device is in use and cannot be uninstalled")
		err = api.NewMounterError(api.ResultCode_DeviceBusy, msg)
		return
	}
	// Force uninstallation kills all container processes on the device.
	if len(processes) > 0 {
		if util.KillRunningProcesses(config, processes) == nil {
			klog.V(3).Infoln("Successfully killed process")
		}
	}

	res := &configs.Resources{SkipDevices: false}
	for i, dev := range deviceInfos {
		klog.V(3).Infoln("Device Rule", "Index", i, "DeviceFile", dev.DeviceFilePath, "Type",
			dev.Type, "Major", dev.Major, "Minor", dev.Minor, "Permissions", dev.Permissions, "Allow", dev.Allow)
		res.Devices = append(res.Devices, &deviceInfos[i].Rule)
	}

	var (
		rollbackRules func() error
		closedFd      func() error
		rollbackFiles func() error
	)

	closedFd, rollbackRules, err = s.DeviceRuleSetFunc(cgroupPath, res)
	// When an error occurs during the execution of steps,
	// roll back the operation in the specified order to ensure atomicity.
	defer func() {
		if err != nil {
			if rollbackFiles != nil {
				rErr := rollbackFiles() // TODO 回滚设备文件，暂时忽略失败
				klog.V(4).Infof("Roll back device files: %v", rErr)
			}
			if rollbackRules != nil {
				rErr := rollbackRules() // TODO 回滚设备权限，暂时忽略回滚失败的情况
				klog.V(4).Infof("Roll back device rules: %v", rErr)
			}
		}
		if closedFd != nil {
			_ = closedFd() // 暂时忽略文件句柄关闭失败的情况
		}
	}()

	if err != nil {
		klog.V(4).ErrorS(err, "Set cgroup device permissions error")
		err = fmt.Errorf("Failed to set access permissions for cgroup devices: %v", err)
		return
	}

	rollbackFiles, err = s.DeleteDeviceFiles(config, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Delete Device Files error")
		err = fmt.Errorf("failed to delete devic files: %v", err)
		return
	}
	err = deviceMounter.ExecutePostUnmountActions(ctx, kubeClient, *config, pod, container, slavePods)
	if err != nil {
		klog.Warningf("execute post unmount actions error: %v", err)
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("failed to execute post unmount actions: %v", err)
		}
		return
	}
	// Get the list of pods that need to be cleaned together with the uninstallation device operation.
	gcPodKeys := deviceMounter.GetPodsToCleanup(ctx, kubeClient, pod, container, slavePods)
	_ = GarbageCollectionPods(s.kubeClient, gcPodKeys)

	message := fmt.Sprintf("Successfully uninstalled %s devices", deviceType)
	s.recorder.Event(pod, v1.EventTypeNormal, "UnMountDevice", message)
	klog.Infoln(deviceType, "UnMountDevice Successfully")
	resp = &api.DeviceResponse{Result: api.ResultCode_Success, Message: message}
	return
}
