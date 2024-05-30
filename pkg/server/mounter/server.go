package mounter

import (
	"context"
	"fmt"
	"time"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/devices"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
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
	pod, err := client.RetryGetPodByName(s.KubeClient, req.PodName, req.PodNamespace, 3)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
			return &api.DeviceResponse{
				Result:  api.ResultCode_NotFound,
				Message: err.Error(),
			}, nil
		} else {
			klog.ErrorS(err, "Get pod failed", "name", req.PodName, "namespace", req.PodNamespace)
			return &api.DeviceResponse{
				Result:  api.ResultCode_Fail,
				Message: err.Error(),
			}, nil
		}
	}
	klog.V(3).InfoS("Get Pod success", "name", req.PodName, "namespace", req.PodNamespace)
	// 校验pod节点
	if pod.Spec.NodeName != s.NodeName {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Pod is not running on the node %s", s.NodeName),
		}, nil
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
	deviceMounter, ok := devices.RegisterDeviceMounter[req.GetDeviceType()]
	if !ok {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Unsupported device type",
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

	// 校验设备资源
	result, msg, ok := deviceMounter.CheckMountResources(s.KubeClient, node, pod, container, resources, req.GetAnnotations())
	if !ok || result != api.ResultCode_Success {
		return &api.DeviceResponse{
			Result:  result,
			Message: msg,
		}, nil
	}

	// 构建slave pods
	// 查找历史同类型的slave pods
	slavePods, _ := s.GetSlavePods(req.GetDeviceType(), pod, container)
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
		slavePods[i] = deepCopy
		s.MutationPodFunc(req.GetDeviceType(), container, pod, deepCopy)
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
		_, _ = s.CreateSlavePodPDB(ctx, slavePod)
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
	time.Sleep(100 * time.Millisecond)
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
	deviceInfos, err := deviceMounter.GetMountDeviceInfo(s.KubeClient, pod, container, readyPods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get mount device info error")
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to detect mount device info: %v", err),
		}, nil
	}
	// 获取容器cgroup路径
	cgroupPath, err := s.GetCGroupPath(pod, container)
	if err != nil {
		klog.V(4).ErrorS(err, "Get cgroup path error")
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	klog.V(4).Infoln("current container cgroup path", cgroupPath)
	pids, err := cgroups.GetAllPids(cgroupPath)
	if err != nil {
		klog.V(4).ErrorS(err, "Get container pids error")
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Error in obtaining container process id: %v", err),
		}, nil
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

	err = deviceMounter.MountDeviceInfoAfter(s.KubeClient, config, pod, container, readyPods)
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
	s.Recorder.Event(pod, v1.EventTypeNormal, "MountDevice", fmt.Sprintf("Successfully mounted %s device", req.GetDeviceType()))
	klog.Infoln("MountDevice Successfully")
	return &api.DeviceResponse{
		Result:  api.ResultCode_Success,
		Message: "Successfully mounted device",
	}, nil
}

func (s *DeviceMounterImpl) UnMountDevice(ctx context.Context, req *api.UnMountDeviceRequest) (*api.DeviceResponse, error) {
	klog.V(4).Infoln("UnMountDevice Called", "Request", req)
	// 查询pod
	if err := CheckUnMountDeviceRequest(req); err != nil {
		klog.V(4).Infoln(err.Error())
		return &api.DeviceResponse{
			Result:  api.ResultCode_Invalid,
			Message: err.Error(),
		}, nil
	}
	pod, err := client.RetryGetPodByName(s.KubeClient, req.PodName, req.PodNamespace, 3)
	if err != nil {
		if apierror.IsNotFound(err) {
			klog.ErrorS(err, "Not found pod", "name", req.PodName, "namespace", req.PodNamespace)
			return &api.DeviceResponse{
				Result:  api.ResultCode_NotFound,
				Message: err.Error(),
			}, nil
		} else {
			klog.ErrorS(err, "Get pod failed", "name", req.PodName, "namespace", req.PodNamespace)
			return &api.DeviceResponse{
				Result:  api.ResultCode_Fail,
				Message: err.Error(),
			}, nil
		}
	}
	klog.V(3).InfoS("Get pod success", "name", req.PodName, "namespace", req.PodNamespace)
	// 校验pod节点
	if pod.Spec.NodeName != s.NodeName {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Pod is not running on the node %s", s.NodeName),
		}, nil
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
	deviceMounter, ok := devices.RegisterDeviceMounter[req.GetDeviceType()]
	if !ok {
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: "Unsupported device type",
		}, nil
	}
	// 查询当前设备类型的slave pods
	slavePods, err := s.GetSlavePods(req.GetDeviceType(), pod, container)
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
	// 获取卸载的设备信息
	deviceInfos, err := deviceMounter.GetUnMountDeviceInfo(s.KubeClient, pod, container, slavePods)
	if err != nil {
		klog.V(4).ErrorS(err, "Get unmount device info error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Failed to detect uninstalled device info: %v", err),
		}, nil
	}
	// 获取容器cgroup路径
	cgroupPath, err := s.GetCGroupPath(pod, container)
	if err != nil {
		klog.V(4).ErrorS(err, "Get cgroup path error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: err.Error(),
		}, nil
	}
	klog.V(4).Infoln("current container cgroup path", cgroupPath)
	// 获取容器pid
	pids, err := cgroups.GetAllPids(cgroupPath)
	if err != nil {
		klog.V(4).ErrorS(err, "Get container pids error")
		return &api.DeviceResponse{
			Result:  api.ResultCode_Fail,
			Message: fmt.Sprintf("Error in obtaining container process id: %v", err),
		}, nil
	}
	klog.V(4).Infoln("current container pids", pids)
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
		if util.KillRunningProcesses(nil, processes) == nil {
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
	config := &util.Config{Target: pids[0], Mount: true}

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

	// 回收slave pod
	slavePodKeys := make([]types.NamespacedName, len(slavePods))
	for i, slavePod := range slavePods {
		slavePodKeys[i] = types.NamespacedName{
			Name:      slavePod.Name,
			Namespace: slavePod.Namespace,
		}
	}
	// TODO 暂时忽略删除失败 （资源泄漏风险）
	_ = RecyclingPods(ctx, s.KubeClient, slavePodKeys)
	// 卸载成功发送event
	s.Recorder.Event(pod, v1.EventTypeNormal, "UnMountDevice", fmt.Sprintf("Successfully uninstalled %s device", req.GetDeviceType()))
	klog.Infoln("UnMountDevice Successfully")
	return &api.DeviceResponse{
		Result:  api.ResultCode_Success,
		Message: "Successfully uninstalled device",
	}, nil
}
