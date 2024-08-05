package npu

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"Ascend-device-plugin/pkg/common"
	"github.com/opencontainers/runc/libcontainer/devices"
	"huawei.com/npu-exporter/v6/common-utils/hwlog"
	"huawei.com/npu-exporter/v6/devmanager"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type AscendNPUMounter struct {
	*NPUCollector
}

func NewAscendNPUMounter() (framework.DeviceMounter, error) {
	klog.Infoln("Creating AscendNPUMounter")

	hwLogConfig := hwlog.LogConfig{
		LogFileName:   "/var/log/mindx-dl/devicePlugin/devicePlugin.log",
		LogLevel:      0,
		MaxBackups:    common.MaxBackups,
		MaxAge:        common.MaxAge,
		MaxLineLength: 1024,
	}
	if err := hwlog.InitRunLogger(&hwLogConfig, context.Background()); err != nil {
		return nil, fmt.Errorf("hwlog init failed, error is %v", err)
	}
	dmgr, err := devmanager.AutoInit("")
	if err != nil {
		klog.Infof("Failed to initialize dcmi: %v.", err)
		return nil, fmt.Errorf("The current node environment does not have the operating conditions for AscendNPUMounter")
	}
	collector := NPUCollector{DeviceManager: dmgr}
	mounter := &AscendNPUMounter{NPUCollector: &collector}
	common.ParamOption = common.Option{
		GetFdFlag:       false,
		UseAscendDocker: true,
		UseVolcanoType:  false,
		AutoStowingDevs: true,
		//ListAndWatchPeriod: *listWatchPeriod,
		PresetVDevice:      true,
		Use310PMixedInsert: false,
		HotReset:           -1,
		//BuildScene:         BuildScene,
		ShareCount:      1,
		RealCardType:    dmgr.GetDevType(),
		LinkdownTimeout: 30,
	}
	klog.Infof("Successfully created AscendNPUMounter, Current device type: %s", mounter.DevType)
	return mounter, nil
}

func (m *AscendNPUMounter) DeviceType() string {
	return "ASCEND_NPU"
}

func (m *AscendNPUMounter) CheckMountResources(
	_ *kubernetes.Clientset,
	node *v1.Node,
	ownerPod *v1.Pod,
	container *api.Container,
	resources map[v1.ResourceName]resource.Quantity,
	annotations, labels map[string]string) (api.ResultCode, string, bool) {

	condition1 := CheckRequest910Resources(resources)
	condition2 := CheckRequestDynamicResources(resources, annotations)
	if !condition1 && !condition2 {
		return api.ResultCode_Fail, "Request for resources error: unsupported resource types", false
	}
	if !util.CheckResourcesInNode(node, resources) {
		return api.ResultCode_Insufficient, "Insufficient node resources", false
	}
	// 校验目标容器是否初始化过npu
	names := ownerPod.Annotations[InitNPUAnnotations]
	ctrNames := strings.Split(strings.TrimSpace(names), ",")
	if slices.Contains(ctrNames, container.Name) {
		return api.ResultCode_Fail, "The target container has initialized the NPU and cannot be mounted again", false
	}
	return api.ResultCode_Success, "", true
}

func (m *AscendNPUMounter) BuildDeviceSlavePodTemplates(
	ownerPod *v1.Pod,
	_ *api.Container,
	request map[v1.ResourceName]resource.Quantity,
	annotations, labels map[string]string,
	_ []*v1.Pod) ([]*v1.Pod, error) {

	slavePod := util.NewDeviceSlavePod(ownerPod, request, annotations, labels)
	// TODO slave pod 不用挂载驱动目录
	env := v1.EnvVar{Name: AscendRuntimeOptionsEnv, Value: "NODRV"}
	slavePod.Spec.Containers[0].Env = append(slavePod.Spec.Containers[0].Env, env)
	slavePod.Spec.PriorityClassName = ownerPod.Spec.PriorityClassName
	return []*v1.Pod{slavePod}, nil
}

func (m *AscendNPUMounter) CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error) {
	if slavePod.Status.Phase == v1.PodRunning {
		return api.Success, nil
	}
	if slavePod.Status.Phase == v1.PodFailed {
		err := fmt.Errorf("device slave container start failed")
		if len(slavePod.Status.Message) > 0 {
			err = fmt.Errorf(slavePod.Status.Message)
		}
		return api.Fail, err
	}
	if !(len(slavePod.Status.Conditions) > 0) {
		return api.Wait, nil
	}
	if slavePod.Status.Conditions[0].Reason == v1.PodReasonUnschedulable ||
		slavePod.Status.Conditions[0].Reason == v1.PodReasonSchedulerError {
		return api.Unschedulable, fmt.Errorf(slavePod.Status.Conditions[0].Message)
	}
	return api.Wait, nil
}

func (m *AscendNPUMounter) GetMountDeviceInfo(
	kubeClient *kubernetes.Clientset,
	ownerPod *v1.Pod,
	container *api.Container,
	slavePods []*v1.Pod) ([]api.DeviceInfo, error) {

	getDevInfoFunc := func(devId int) (api.DeviceInfo, error) {
		deviceId := strconv.Itoa(devId)
		deviceFilePath := ASCEND_DEVICE_FILE_PREFIX + deviceId
		if IsVirtDev(devId) {
			deviceFilePath = ASCEND_VDEVICE_FILE_PREFIX + deviceId
		}
		major, minor, devType, err := util.GetDeviceFileVersionV2(deviceFilePath)
		if err != nil {
			return api.DeviceInfo{}, err
		}
		return api.DeviceInfo{
			DeviceID:       deviceId,
			DeviceFilePath: deviceFilePath,
			Rule: devices.Rule{
				Type:        devType,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		}, nil
	}
	deviceInfos, err := m.GetSlavePodsDeviceInfo(kubeClient, slavePods, getDevInfoFunc)
	if err != nil {
		return deviceInfos, err
	}
	// TODO 如果原始pod有npu，不再重复插入管理设备
	if HasNPU(ownerPod, container) {
		return deviceInfos, nil
	}
	// TODO 插入npu管理设备
	mgrDevice := []string{ASCEND_DAVINCI_MANAGER_PATH, ASCEND_DEVMM_SVM_FILE_PATH, ASCEND_HISI_HDC_FILE_PATH}
	for _, deviceFile := range mgrDevice {
		major, minor, devType, err := util.GetDeviceFileVersionV2(deviceFile)
		if err != nil {
			return deviceInfos, err
		}
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceFilePath: deviceFile,
			Rule: devices.Rule{
				Type:        devType,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		})
	}
	return deviceInfos, nil
}

func (m *AscendNPUMounter) MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, _ util.Config, ownerPod *v1.Pod, container *api.Container, _ []*v1.Pod) error {
	if !HasNPU(ownerPod, container) {
		var contNames []string
		names := ownerPod.Annotations[InitNPUAnnotations]
		if names = strings.TrimSpace(names); len(names) > 0 {
			contNames = strings.Split(names, ",")
		}
		if !slices.Contains(contNames, container.Name) {
			contNames = append(contNames, container.Name)
			annotations := map[string]string{InitNPUAnnotations: strings.Join(contNames, ",")}
			if err := client.PatchPodAnnotations(kubeClient, ownerPod, annotations); err != nil {
				return fmt.Errorf("Failed to patch pod annotation [%s]: %v", InitNPUAnnotations, err)
			}
		}
	}
	return nil
}

func (m *AscendNPUMounter) GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	if HasNPU(ownerPod, container) {
		return nil, fmt.Errorf("Currently not supported for uninstalling Ascend NPUs")
	}
	devInfos, err := m.GetSlavePodsDeviceInfo(kubeClient, slavePods, func(devId int) (api.DeviceInfo, error) {
		deviceId := strconv.Itoa(devId)
		deviceFilePath := ASCEND_DEVICE_FILE_PREFIX + deviceId
		if IsVirtDev(devId) {
			deviceFilePath = ASCEND_VDEVICE_FILE_PREFIX + deviceId
		}
		major, minor, devType, err := util.GetDeviceFileVersionV2(deviceFilePath)
		if err != nil {
			return api.DeviceInfo{}, err
		}
		return api.DeviceInfo{
			DeviceID:       deviceId,
			DeviceFilePath: deviceFilePath,
			Rule: devices.Rule{
				Type:        devType,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       false,
			},
		}, nil
	})
	if err != nil {
		return nil, err
	}

	// TODO 移除npu管理设备
	mgrDevice := []string{ASCEND_DAVINCI_MANAGER_PATH, ASCEND_DEVMM_SVM_FILE_PATH, ASCEND_HISI_HDC_FILE_PATH}
	for _, deviceFile := range mgrDevice {
		major, minor, devType, err := util.GetDeviceFileVersionV2(deviceFile)
		if err != nil {
			return nil, err
		}
		devInfos = append(devInfos, api.DeviceInfo{
			DeviceFilePath: deviceFile,
			Rule: devices.Rule{
				Type:        devType,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       false,
			},
		})
	}

	return devInfos, nil
}

// TODO 昇腾npu以容器命名空间隔离进程id，经测试发现有版本兼容问题
// Mindx v6.0+配套软件下：也就是ascend驱动24.1+、cann8.0+， host进程命名空间能检测到容器进程
// 版本配套信息 https://www.hiascend.com/document/detail/zh/mindx-dl/60rc2/description/releasenote/mxreleasenote_006.html
// Mindx v6.0-配套软件下：host进程命名空间无法查询到容器进程，强烈建议升级Mindx配套软件到v6.0+
// 版本配套信息 https://www.hiascend.com/document/detail/zh/mindx-dl/501/releasenote/mxreleasenote_002.html
func (m *AscendNPUMounter) GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
	processInfos, err := m.GetRunningProcess()
	if err != nil {
		return nil, err
	}
	processes := sets.NewInt()
	for _, processInfo := range processInfos {
		for i := int32(0); i < processInfo.ProcNum; i++ {
			info := processInfo.DevProcArray[i]
			if slices.Contains(containerPids, int(info.Pid)) {
				processes.Insert(int(info.Pid))
			}
		}
	}
	return processes.List(), nil
}

// 卸载设备成功前的后续动作
func (m *AscendNPUMounter) UnMountDeviceInfoAfter(kubeClient *kubernetes.Clientset, _ util.Config, ownerPod *v1.Pod, container *api.Container, _ []*v1.Pod) error {
	if names := strings.TrimSpace(ownerPod.Annotations[InitNPUAnnotations]); len(names) > 0 {
		oldNames := strings.Split(names, ",")
		newNames := util.DeleteSliceFunc(oldNames, func(s string) bool {
			return s != container.Name
		})
		if len(oldNames) != len(newNames) {
			annotations := map[string]string{InitNPUAnnotations: strings.Join(newNames, ",")}
			if err := client.PatchPodAnnotations(kubeClient, ownerPod, annotations); err != nil {
				return fmt.Errorf("Failed to patch pod annotation [%s]: %v", InitNPUAnnotations, err)
			}
		}
	}
	return nil
}

func (m *AscendNPUMounter) CleanupPodResources(_ *kubernetes.Clientset,
	_ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) []types.NamespacedName {

	slavePodKeys := make([]types.NamespacedName, len(slavePods))
	for i, slavePod := range slavePods {
		slavePodKeys[i] = types.NamespacedName{
			Name:      slavePod.Name,
			Namespace: slavePod.Namespace,
		}
	}
	return slavePodKeys
}
