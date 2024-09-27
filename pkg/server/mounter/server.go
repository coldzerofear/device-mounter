package mounter

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencontainers/runc/libcontainer/configs"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

type DeviceMounterImpl struct {
	api.UnimplementedDeviceMountServiceServer
	NodeName   string
	KubeClient *kubernetes.Clientset
	Recorder   record.EventRecorder
	NodeLister listerv1.NodeLister
	PodLister  listerv1.PodLister
	IsCGroupV2 bool
}

func (s *DeviceMounterImpl) MountDevice(ctx context.Context, req *api.MountDeviceRequest) (resp *api.DeviceResponse, err error) {
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

	err = CheckMountDeviceRequest(req)
	if err != nil {
		klog.V(4).Infoln(err.Error())
		return
	}

	// 查询目标pod
	var pod *v1.Pod
	pod, err = s.GetTargetPod(ctx, req.PodName, req.PodNamespace)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
			resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		} else {
			klog.ErrorS(err, "Get target Pod failed", "name", req.PodName, "namespace", req.PodNamespace)
		}
		return
	}

	// 校验容器是否存在
	var container *api.Container
	container, err = CheckPodContainer(pod, req.GetContainer())
	if err != nil {
		return
	}

	// 校验容器是否运行
	err = CheckPodContainerStatus(pod, container)
	if err != nil {
		return
	}

	// 校验设备类型
	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.GetDeviceMounter(deviceType)
	if !ok {
		err = fmt.Errorf("Unsupported device type: %s", req.GetDeviceType())
		return
	}

	// 资源格式转换
	resources := make(map[v1.ResourceName]resource.Quantity)
	var quantity resource.Quantity
	for key, val := range req.GetResources() {
		quantity, err = resource.ParseQuantity(val)
		if err != nil {
			return
		}
		resources[v1.ResourceName(key)] = quantity
	}

	// 查询node
	node, err := s.NodeLister.Get(s.NodeName)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found target node", "name", s.NodeName)
			resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		} else {
			klog.ErrorS(err, "Get target node failed", "name", s.NodeName)
		}
		return
	}

	node = node.DeepCopy()
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)

	// 校验设备资源
	err = deviceMounter.ValidateMountRequest(ctx, kubeClient, node, pod, container, resources, req.GetAnnotations(), req.GetLabels())
	if err != nil {
		klog.V(3).ErrorS(err, "validate mount request failed")
		return
	}

	// 构建slave pods
	// 查找历史同类型的slave pods
	var slavePods []*v1.Pod
	slavePods, err = s.GetSlavePods(deviceType, pod, container)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found slave pods")
			resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		} else {
			klog.ErrorS(err, "Get slave pods failed")
		}
		return
	}

	slavePods, err = deviceMounter.BuildSupportPodTemplates(ctx, pod, container, resources, req.GetAnnotations(), req.GetLabels(), slavePods)
	if err != nil {
		klog.V(3).ErrorS(err, "Build device slave pods failed")
		return
	}
	// 变异配置
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
	// 创建slave pods
	for _, slavePod := range slavePods {
		slavePod, err = s.KubeClient.CoreV1().Pods(slavePod.Namespace).Create(ctx, slavePod, metav1.CreateOptions{})
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
			// 发生错误回收已创建的Slave Pod
			// TODO 暂时忽略删除失败 （设备泄漏风险）
			_ = GarbageCollectionPods(s.KubeClient, slavePodKeys)
		}
		if closedFd != nil {
			_ = closedFd() // 暂时忽略文件句柄关闭失败的情况
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
	// 校验slave pods准备就绪
	readyPods, skipPods, err = WaitSupportPodsReady(ctx, s.PodLister, s.KubeClient, deviceMounter, slavePodKeys)
	if err != nil {
		klog.V(4).ErrorS(err, "Wait slave pods ready failed")
		return
	}
	// 如果此时没有ready的pod则返回错误
	if len(readyPods) == 0 {
		err = fmt.Errorf("Waiting for device pod to be ready failed")
		return
	}

	var deviceInfos []api.DeviceInfo
	// 获取挂载设备信息
	deviceInfos, err = deviceMounter.GetDeviceInfosToMount(ctx, kubeClient, pod, container, readyPods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get mount device info error")
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("Failed to detect mount device info: %v", err)
		}
		return
	}

	// 获取目标容器的cgroup路径和pid
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
		klog.V(4).ErrorS(err, "Set cgroup device permissions error")
		err = fmt.Errorf("Failed to set access permissions for cgroup devices: %v", err)
		return
	}
	// 创建设备文件
	config := &util.Config{Target: pids[0], Mount: true}

	rollbackFiles, err = s.CreateDeviceFiles(config, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Create Device Files error")
		err = fmt.Errorf("Failed to create devic files: %v", err)
		return
	}

	err = deviceMounter.ExecutePostMountActions(ctx, kubeClient, *config, pod, container, readyPods)
	if err != nil {
		klog.Warningf("execute post mount actions error: %v", err)
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("Failed to execute post mount actions: %v", err)
		}
		return
	}
	// 成功前回收跳过的pod
	skipPodKeys := make([]api.ObjectKey, len(skipPods))
	for i, skipPod := range skipPods {
		skipPodKeys[i] = api.ObjectKeyFromObject(skipPod)
	}
	// 回收跳过的slave pods
	_ = GarbageCollectionPods(s.KubeClient, skipPodKeys)
	// 挂载成功发送event
	message := fmt.Sprintf("Successfully mounted %s devices", deviceType)
	s.Recorder.Event(pod, v1.EventTypeNormal, "MountDevice", message)
	klog.Infoln(deviceType, "MountDevice Successfully")
	resp = &api.DeviceResponse{Result: api.ResultCode_Success, Message: message}
	return
}

func (s *DeviceMounterImpl) UnMountDevice(ctx context.Context, req *api.UnMountDeviceRequest) (resp *api.DeviceResponse, err error) {
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

	err = CheckUnMountDeviceRequest(req)
	if err != nil {
		klog.V(4).Infoln(err.Error())
		return
	}

	// 查询目标pod
	var pod *v1.Pod
	pod, err = s.GetTargetPod(ctx, req.PodName, req.PodNamespace)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
			resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		} else {
			klog.ErrorS(err, "Get target Pod failed", "name", req.PodName, "namespace", req.PodNamespace)
		}
		return
	}

	// 校验容器是否存在
	var container *api.Container
	container, err = CheckPodContainer(pod, req.GetContainer())
	if err != nil {
		return
	}

	// 校验容器是否运行
	err = CheckPodContainerStatus(pod, container)
	if err != nil {
		return
	}

	// 校验设备类型
	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.GetDeviceMounter(deviceType)
	if !ok {
		err = fmt.Errorf("Unsupported device type: %s", req.GetDeviceType())
		return
	}

	// 查询当前设备类型的slave pods
	slavePods, err := s.GetSlavePods(deviceType, pod, container)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found slave pods")
			resp = &api.DeviceResponse{Result: api.ResultCode_NotFound, Message: err.Error()}
		} else {
			klog.ErrorS(err, "Get slave pods failed")
		}
		return
	}
	if len(slavePods) == 0 {
		msg := fmt.Sprintf("No device found for uninstallation")
		err = api.NewMounterError(api.ResultCode_NotFound, msg)
		return
	}
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)

	// 获取卸载的设备信息
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
	// 获取目标容器的cgroup路径和pid
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
	if !req.GetForce() && len(processes) > 0 {
		// 非强制卸载情况下设备上有再运行的进程，提示错误
		msg := fmt.Sprintf("The device is in use and cannot be uninstalled")
		err = api.NewMounterError(api.ResultCode_DeviceBusy, msg)
		return
	}
	// kill processes
	if len(processes) > 0 {
		// TODO 暂且忽略失败的情况
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

	// 设备权限摘除
	closedFd, rollbackRules, err = s.DeviceRuleSetFunc(cgroupPath, res)
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
	// 删除设备文件
	rollbackFiles, err = s.DeleteDeviceFiles(config, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Delete Device Files error")
		err = fmt.Errorf("Failed to delete devic files: %v", err)
		return
	}
	err = deviceMounter.ExecutePostUnmountActions(ctx, kubeClient, *config, pod, container, slavePods)
	if err != nil {
		klog.Warningf("execute post unmount actions error: %v", err)
		if _, ok = err.(*api.MounterError); !ok {
			err = fmt.Errorf("Failed to execute post unmount actions: %v", err)
		}
		return
	}
	// 回收slave pod
	gcPodKeys := deviceMounter.GetPodsToCleanup(ctx, kubeClient, pod, container, slavePods)
	// TODO 暂时忽略删除失败 （有资源泄漏风险）
	_ = GarbageCollectionPods(s.KubeClient, gcPodKeys)
	// 卸载成功发送event
	message := fmt.Sprintf("Successfully uninstalled %s devices", deviceType)
	s.Recorder.Event(pod, v1.EventTypeNormal, "UnMountDevice", message)
	klog.Infoln(deviceType, "UnMountDevice Successfully")
	resp = &api.DeviceResponse{Result: api.ResultCode_Success, Message: message}
	return
}
