package npu

import (
	"context"
	"fmt"
	"strconv"

	"Ascend-device-plugin/pkg/common"
	"github.com/opencontainers/runc/libcontainer/devices"
	"huawei.com/npu-exporter/v6/common-utils/hwlog"
	"huawei.com/npu-exporter/v6/devmanager"
	"k8s-device-mounter/pkg/api"
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
	mounter := &AscendNPUMounter{}
	hwLogConfig := hwlog.LogConfig{
		LogFileName:   "/var/log/mindx-dl/devicePlugin/devicePlugin.log",
		LogLevel:      0,
		MaxBackups:    common.MaxBackups,
		MaxAge:        common.MaxAge,
		MaxLineLength: 1024,
	}
	if err := hwlog.InitRunLogger(&hwLogConfig, context.Background()); err != nil {
		return mounter, fmt.Errorf("hwlog init failed, error is %v", err)
	}
	dmgr, err := devmanager.AutoInit("")
	if err != nil {
		klog.Infof("Failed to initialize dcmi: %v.", err)
		return mounter, fmt.Errorf("The current node environment does not have the operating conditions for AscendNPUMounter")
	}
	mounter.NPUCollector = &NPUCollector{
		DeviceManager: dmgr,
	}
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

func (m *AscendNPUMounter) CheckMountResources(_ *kubernetes.Clientset, node *v1.Node, _ *v1.Pod, _ *api.Container, request map[v1.ResourceName]resource.Quantity, _ map[string]string) (api.ResultCode, string, bool) {
	condition1 := CheckRequest910Resources(request)
	condition2 := CheckRequestDynamicResources(request)
	if !condition1 && !condition2 {
		return api.ResultCode_Fail, "Request for resources error: unsupported resource types", false
	}
	if !util.CheckResourcesInNode(node, request) {
		return api.ResultCode_Insufficient, "Insufficient node resources", false
	}
	return api.ResultCode_Success, "", true
}

func (m *AscendNPUMounter) BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, _ *api.Container,
	request map[v1.ResourceName]resource.Quantity, annotations map[string]string, _ []*v1.Pod) ([]*v1.Pod, error) {
	return []*v1.Pod{util.NewDeviceSlavePod(ownerPod, request, annotations)}, nil
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
	if slavePod.Status.Conditions[0].Reason == v1.PodReasonUnschedulable {
		return api.Unschedulable, fmt.Errorf(slavePod.Status.Conditions[0].Message)
	}
	return api.Wait, nil
}

func (m *AscendNPUMounter) GetMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	deviceInfos, err := m.GetSlavePodsDeviceInfo(kubeClient, slavePods, func(devId int) (api.DeviceInfo, error) {
		deviceId := strconv.Itoa(devId)
		deviceFilePath := ASCEND_DEVICE_FILE_PREFIX + deviceId
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
	})
	if err != nil {
		return deviceInfos, err
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

func (m *AscendNPUMounter) MountDeviceInfoAfter(_ *kubernetes.Clientset, _ util.Config, _ *v1.Pod, _ *api.Container, _ []*v1.Pod) error {
	return nil
}

func (m *AscendNPUMounter) GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, _ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	return nil, fmt.Errorf("Currently not supported for uninstalling Ascend NPUs")
	//return m.GetSlavePodsDeviceInfo(kubeClient, slavePods, func(devId int) (api.DeviceInfo, error) {
	//	deviceId := strconv.Itoa(devId)
	//	deviceFilePath := ASCEND_DEVICE_FILE_PREFIX + deviceId
	//	major, minor, devType, err := util.GetDeviceFileVersionV2(deviceFilePath)
	//	if err != nil {
	//		return api.DeviceInfo{}, err
	//	}
	//	return api.DeviceInfo{
	//		DeviceID:       deviceId,
	//		DeviceFilePath: deviceFilePath,
	//		Rule: devices.Rule{
	//			Type:        devType,
	//			Major:       int64(major),
	//			Minor:       int64(minor),
	//			Permissions: DEFAULT_CGROUP_PERMISSION,
	//			Allow:       false,
	//		},
	//	}, nil
	//})
}

// TODO 由于昇腾npu以命名空间隔离进程id，在host命名空间下无法查看容器进程id，所以这种方法不适用
//func (m *AscendNPUMounter) GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
//	processInfos, err := m.GetRunningProcess()
//	if err != nil {
//		return nil, err
//	}
//	processes := sets.NewInt()
//	for _, processInfo := range processInfos {
//		for i := int32(0); i < processInfo.ProcNum; i++ {
//			info := processInfo.DevProcArray[i]
//			if util.ContainsInt(containerPids, int(info.Pid)) {
//				processes.Insert(int(info.Pid))
//			}
//		}
//	}
//	return processes.List(), nil
//}

func (m *AscendNPUMounter) GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
	processes := sets.NewInt()

	return processes.List(), nil
}

// 卸载设备成功前的后续动作
func (m *AscendNPUMounter) UnMountDeviceInfoAfter(_ *kubernetes.Clientset, _ util.Config, _ *v1.Pod, _ *api.Container, _ []*v1.Pod) error {
	return nil
}

func (m *AscendNPUMounter) RecycledPodResources(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) []types.NamespacedName {
	slavePodKeys := make([]types.NamespacedName, len(slavePods))
	for i, slavePod := range slavePods {
		slavePodKeys[i] = types.NamespacedName{
			Name:      slavePod.Name,
			Namespace: slavePod.Namespace,
		}
	}
	return slavePodKeys
}
