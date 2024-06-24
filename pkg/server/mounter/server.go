package mounter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opencontainers/runc/libcontainer/configs"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	PodLister  listerv1.PodLister
	IsCGroupV2 bool
}

func (s *DeviceMounterImpl) MountDevice(ctx context.Context, req *api.MountDeviceRequest) (*api.DeviceResponse, error) {
	klog.V(4).Infoln("MountDevice Called", "Request", req)
	if err := CheckMountDeviceRequest(req); err != nil {
		klog.V(4).Infoln(err.Error())
		return &api.DeviceResponse{
			Result:  api.ResultCode_Invalid,
			Message: err.Error(),
		}, nil
	}
	// 查询pod
	pod, resp := s.GetTargetPod(req.PodName, req.PodNamespace)
	if resp != nil {
		return resp, nil
	}

	// 校验容器是否存在
	container, err := CheckPodContainer(pod, req.GetContainer())
	if err != nil {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Invalid,
			Message: err.Error(),
		}, nil
	}

	// 校验容器是否运行
	if err = CheckPodContainerStatus(pod, container); err != nil {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}

	// 校验设备类型
	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.RegisterDeviceMounter[deviceType]
	if !ok {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Unsupported device type: " + req.GetDeviceType(),
		}, nil
	}

	// 资源格式转换
	resources := make(map[v1.ResourceName]resource.Quantity)
	for key, val := range req.GetResources() {
		quantity, err := resource.ParseQuantity(val)
		if err != nil {
			return &api.DeviceResponse{
				Result:  api.ResultCode_Fail,
				Message: fmt.Sprintf("The value format of resource [%s] is correct", key),
			}, nil
		}
		resources[v1.ResourceName(key)] = quantity
	}

	// 查询node
	node, err := s.KubeClient.CoreV1().Nodes().Get(ctx, s.NodeName, metav1.GetOptions{})
	if err != nil {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)

	// 校验设备资源
	result, msg, ok := deviceMounter.CheckMountResources(kubeClient, node, pod, container, resources, req.GetAnnotations())
	if !ok || result != api.ResultCode_Success {
		return &api.DeviceResponse{
			Result:  result,
			Message: msg,
		}, nil
	}

	// 构建slave pods
	// 查找历史同类型的slave pods
	slavePods, _ := s.GetSlavePods(deviceType, pod, container)
	slavePods, err = deviceMounter.BuildDeviceSlavePodTemplates(pod, container, resources, req.GetAnnotations(), slavePods)
	if err != nil {
		klog.V(3).ErrorS(err, "Build device slave pods failed")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	// 变异
	for i, slavePod := range slavePods {
		deepCopy := slavePod.DeepCopy()
		s.MutationPodFunc(deviceType, container, pod, deepCopy)
		deepCopy, err = s.PatchPod(deepCopy, req.GetPatches())
		if err != nil {
			return &api.DeviceResponse{
				Result:  api.ResultCode_Fail,
				Message: err.Error(),
			}, nil
		}
		slavePods[i] = deepCopy
	}
	// 创建slave pods
	var slavePodKeys []types.NamespacedName
	for _, slavePod := range slavePods {
		slavePod, err = s.KubeClient.CoreV1().Pods(slavePod.Namespace).
			Create(ctx, slavePod, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Create Slave Pod failed: %v", err)
			break
		}
		slavePodKeys = append(slavePodKeys, types.NamespacedName{
			Name:      slavePod.Name,
			Namespace: slavePod.Namespace,
		})
		_, _ = s.CreateSlavePodDisruptionBudget(ctx, slavePod)
	}
	if err != nil || len(slavePodKeys) == 0 {
		// 创建失败 回收已创建的pod
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Device slave pod creation failed",
		}, nil
	}
	klog.Infoln("Successfully created slave pods", slavePodKeys)
	timeout := time.Duration(10) // 默认值 10 秒
	if req.GetTimeoutSeconds() > 0 {
		timeout = time.Duration(req.GetTimeoutSeconds())
	}
	// 校验slave pods准备就绪
	readyPods, skipPods, resCode, err := WaitSlavePodsReady(ctx,
		s.PodLister, s.KubeClient, deviceMounter, timeout, slavePodKeys)
	if err != nil {
		klog.V(4).ErrorS(err, "Wait slave pods ready error")
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  resCode,
			Message: err.Error(),
		}, nil
	}
	// 如果此时没有ready的pod则返回错误
	if len(readyPods) == 0 {
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Preparing device slave pod failed",
		}, nil
	}

	// 获取挂载设备信息
	deviceInfos, err := deviceMounter.GetMountDeviceInfo(kubeClient, pod, container, readyPods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get mount device info error")
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to detect mount device info: %v", err),
		}, nil
	}
	// 获取目标容器的cgroup路径和pid
	pids, cgroupPath, resp := s.GetContainerCGroupPathAndPids(pod, container)
	if resp != nil {
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return resp, nil
	}
	klog.V(4).Infoln("current container pids", pids)

	res := &configs.Resources{SkipDevices: false}
	for i, dev := range deviceInfos {
		klog.V(3).Infoln("Device Rule", "Index", i, "DeviceFile", dev.DeviceFilePath, "Type",
			dev.Type, "Major", dev.Major, "Minor", dev.Minor, "Permissions", dev.Permissions, "Allow", dev.Allow)
		res.Devices = append(res.Devices, &deviceInfos[i].Rule)
	}

	closed, rollbackRules, err := s.DeviceRuleSetFunc(cgroupPath, res)
	defer closed()
	if err != nil {
		klog.V(4).ErrorS(err, "Set cgroup device permissions error")
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to set access permissions for cgroup devices: %v", err),
		}, nil
	}
	// 创建设备文件
	config := &util.Config{Target: pids[0], Mount: true}

	rollbackFiles, err := s.CreateDeviceFiles(config, deviceInfos)
	if err != nil {
		// TODO 回滚设备文件，忽略失败
		_ = rollbackFiles()
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		// TODO 回收pod, 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to create devic files: %v", err),
		}, nil
	}

	err = deviceMounter.MountDeviceInfoAfter(kubeClient, *config, pod, container, readyPods)
	if err != nil {
		klog.Warningf("Device mounting follow-up action failed: %v", err)
		// TODO 回滚设备文件，忽略失败
		_ = rollbackFiles()
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to mount device information after: %v", err),
		}, nil
	}
	// 成功前回收掉跳过的pod
	skipPodKeys := make([]types.NamespacedName, len(skipPods))
	for i, skipPod := range skipPods {
		skipPodKeys[i] = types.NamespacedName{
			Name:      skipPod.Name,
			Namespace: skipPod.Namespace,
		}
	}
	_ = RecyclingPods(ctx, s.KubeClient, skipPodKeys)
	// 挂载成功发送event
	s.Recorder.Event(pod, v1.EventTypeNormal, "MountDevice", fmt.Sprintf("Successfully mounted %s device", deviceType))
	klog.Infoln("MountDevice Successfully")
	return &api.DeviceResponse{
		Result:  api.ResultCode_Success,
		Message: "Successfully mounted device",
	}, nil
}

func (s *DeviceMounterImpl) UnMountDevice(ctx context.Context, req *api.UnMountDeviceRequest) (*api.DeviceResponse, error) {
	klog.V(4).Infoln("UnMountDevice Called", "Request", req)
	if err := CheckUnMountDeviceRequest(req); err != nil {
		klog.V(4).Infoln(err.Error())
		return &api.DeviceResponse{
			Result:  api.ResultCode_Invalid,
			Message: err.Error(),
		}, nil
	}
	// 查询pod
	pod, resp := s.GetTargetPod(req.PodName, req.PodNamespace)
	if resp != nil {
		return resp, nil
	}

	// 校验容器是否存在
	container, err := CheckPodContainer(pod, req.GetContainer())
	if err != nil {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Invalid,
			Message: err.Error(),
		}, nil
	}

	// 校验容器是否运行
	if err = CheckPodContainerStatus(pod, container); err != nil {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	// 校验设备类型
	deviceType := strings.ToUpper(req.GetDeviceType())
	deviceMounter, ok := framework.RegisterDeviceMounter[deviceType]
	if !ok {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Unsupported device type: " + req.GetDeviceType(),
		}, nil
	}
	// 查询当前设备类型的slave pods
	slavePods, err := s.GetSlavePods(deviceType, pod, container)
	if err != nil {
		klog.V(4).ErrorS(err, "Get slave pods error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	if len(slavePods) == 0 {
		return &api.DeviceResponse{
			Result:  api.ResultCode_NotFound,
			Message: fmt.Sprintf("No device found for uninstallation"),
		}, nil
	}
	kubeConfig := client.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)
	// 获取卸载的设备信息
	deviceInfos, err := deviceMounter.GetUnMountDeviceInfo(kubeClient, pod, container, slavePods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get unmount device info error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to detect uninstalled device info: %v", err),
		}, nil
	}
	// 获取目标容器的cgroup路径和pid
	pids, cgroupPath, resp := s.GetContainerCGroupPathAndPids(pod, container)
	if resp != nil {
		return resp, nil
	}
	klog.V(4).Infoln("current container pids", pids)

	config := &util.Config{Target: pids[0], Mount: true}
	processes, err := deviceMounter.GetDeviceRunningProcesses(pids, deviceInfos)
	if err != nil {
		klog.V(4).ErrorS(err, "Get device running processes error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to detect uninstalled device info: %v", err),
		}, nil
	}
	if !req.GetForce() && len(processes) > 0 {
		// 非强制卸载情况下设备上有再运行的进程，提示错误
		return &api.DeviceResponse{
			Result:  api.ResultCode_DeviceBusy,
			Message: fmt.Sprintf("The device is in use and cannot be uninstalled"),
		}, nil
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

	// 设备权限摘除
	closed, rollbackRules, err := s.DeviceRuleSetFunc(cgroupPath, res)
	defer closed()
	if err != nil {
		klog.V(4).ErrorS(err, "Set cgroup device permissions error")
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to set access permissions for cgroup devices: %v", err),
		}, nil
	}
	// 删除设备文件
	rollbackFiles, err := s.DeleteDeviceFiles(config, deviceInfos)
	if err != nil {
		// TODO 回滚设备文件，忽略失败
		_ = rollbackFiles()
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to set access permissions for cgroup devices: %v", err),
		}, nil
	}
	err = deviceMounter.UnMountDeviceInfoAfter(kubeClient, *config, pod, container, slavePods)
	if err != nil {
		klog.Warningf("Device unmounting follow-up action failed: %v", err)
		// TODO 回滚设备文件，忽略失败
		_ = rollbackFiles()
		// TODO 回滚设备权限，暂时忽略回滚失败的情况
		_ = rollbackRules()
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to unmount device information after: %v", err),
		}, nil
	}
	// 回收slave pod
	recyclingPodkeys := deviceMounter.RecycledPodResources(kubeClient, pod, container, slavePods)
	// TODO 暂时忽略删除失败 （有资源泄漏风险）
	_ = RecyclingPods(ctx, s.KubeClient, recyclingPodkeys)
	// 卸载成功发送event
	s.Recorder.Event(pod, v1.EventTypeNormal, "UnMountDevice", fmt.Sprintf("Successfully uninstalled %s device", deviceType))
	klog.Infoln("UnMountDevice Successfully")
	return &api.DeviceResponse{
		Result:  api.ResultCode_Success,
		Message: "Successfully uninstalled device",
	}, nil
}
