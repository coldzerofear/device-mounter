package gpu

import (
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/opencontainers/runc/libcontainer/devices"
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

type NvidiaGPUMounter struct {
	*GPUCollector
}

func NewNvidiaGPUMounter() (framework.DeviceMounter, error) {
	klog.Infoln("Creating NvidiaGPUMounter")
	if !checkDeviceEnvironment() {
		return nil, fmt.Errorf("The current node environment does not have the operating conditions for NvidiaGPUMounter")
	}
	collector, err := NewGPUCollector()
	if err != nil {
		return nil, err
	}
	mounter := &NvidiaGPUMounter{GPUCollector: collector}
	klog.Infoln("Successfully created NvidiaGPUMounter")
	return mounter, nil
}

func (m *NvidiaGPUMounter) DeviceType() string {
	return "NVIDIA_GPU"
}

func checkDeviceEnvironment() bool {
	if rt := nvml.Init(); rt != nvml.SUCCESS {
		klog.Infof("Failed to initialize NVML: %s.", nvml.ErrorString(rt))
		klog.Infof("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
		klog.Infof("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		klog.Infof("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
		klog.Infof("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
		return false
	}
	defer nvml.Shutdown()
	return true
}

func (m *NvidiaGPUMounter) CheckMountResources(
	_ *kubernetes.Clientset,
	node *v1.Node,
	_ *v1.Pod,
	_ *api.Container,
	request map[v1.ResourceName]resource.Quantity,
	_ map[string]string) (api.ResultCode, string, bool) {

	if !util.CheckResourcesInSlice(request, []string{ResourceName}, nil) {
		return api.ResultCode_Fail, "Request for resources error", false
	}
	if !util.CheckResourcesInNode(node, request) {
		return api.ResultCode_Insufficient, "Insufficient node resources", false
	}
	return api.ResultCode_Success, "", true
}

func (m *NvidiaGPUMounter) BuildDeviceSlavePodTemplates(
	ownerPod *v1.Pod,
	_ *api.Container,
	request map[v1.ResourceName]resource.Quantity,
	annotations map[string]string,
	_ []*v1.Pod) ([]*v1.Pod, error) {

	quantity := request[ResourceName]
	gpuNumber := quantity.Value()
	limits := map[v1.ResourceName]resource.Quantity{
		ResourceName: resource.MustParse("1"),
	}
	var slavePods []*v1.Pod
	for i := int64(0); i < gpuNumber; i++ {
		slavePod := util.NewDeviceSlavePod(ownerPod, limits, annotations)
		// TODO 让创建出来的slave pod只占用gpu，不包含设备文件
		env := v1.EnvVar{Name: NVIDIA_VISIBLE_DEVICES_ENV, Value: "none"}
		slavePod.Spec.Containers[0].Env = append(slavePod.Spec.Containers[0].Env, env)
		slavePod.Spec.PriorityClassName = ownerPod.Spec.PriorityClassName
		slavePods = append(slavePods, slavePod)
	}
	return slavePods, nil
}

func (m *NvidiaGPUMounter) CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error) {
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

func (m *NvidiaGPUMounter) GetMountDeviceInfo(_ *kubernetes.Clientset, ownerPod *v1.Pod,
	container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {

	var deviceInfos []api.DeviceInfo
	var gpus []*NvidiaGPU
	for _, slavePod := range slavePods {
		resources, err := m.GetPodGPUResources(slavePod.Name, slavePod.Namespace)
		if err != nil {
			return deviceInfos, err
		}
		gpus = append(gpus, resources...)
	}
	// nvidia c 195:x
	for _, gpu := range gpus {
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceID:       gpu.UUID,
			DeviceFilePath: gpu.DeviceFilePath,
			Rule: devices.Rule{
				Type:        devices.CharDevice,
				Major:       DEFAULT_NVIDIA_MAJOR_NUMBER,
				Minor:       int64(gpu.MinorNumber),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		})
	}
	ownerGPUResources, _ := m.GetContainerGPUResources(ownerPod.Name, ownerPod.Namespace, container.Name)
	// TODO 原始pod上没有gpu则挂载gpu驱动相关设备文件
	if !(len(ownerGPUResources) > 0) {
		// nvidiactl c 195:255
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceFilePath: NVIDIA_NVIDIACTL_FILE_PATH,
			Rule: devices.Rule{
				Type:        devices.CharDevice,
				Major:       DEFAULT_NVIDIA_MAJOR_NUMBER,
				Minor:       DEFAULT_NVIDIACTL_MINOR_NUMBER,
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		})
		// nvidia-uvm c x:x
		major, minor, err := util.GetDeviceFileVersion(NVIDIA_NVIDIA_UVM_FILE_PATH)
		if err != nil {
			return deviceInfos, err
		}
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceFilePath: NVIDIA_NVIDIA_UVM_FILE_PATH,
			Rule: devices.Rule{
				Type:        devices.CharDevice,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		})
		// nvidia-uvm-tools c x:x
		major, minor, err = util.GetDeviceFileVersion(NVIDIA_NVIDIA_UVM_TOOLS_FILE_PATH)
		if err != nil {
			return deviceInfos, err
		}
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceFilePath: NVIDIA_NVIDIA_UVM_TOOLS_FILE_PATH,
			Rule: devices.Rule{
				Type:        devices.CharDevice,
				Major:       int64(major),
				Minor:       int64(minor),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       true,
			},
		})
	}
	return deviceInfos, nil
}

func (m *NvidiaGPUMounter) MountDeviceInfoAfter(_ *kubernetes.Clientset, _ util.Config, _ *v1.Pod, _ *api.Container, _ []*v1.Pod) error {
	return nil
}

func (m *NvidiaGPUMounter) GetUnMountDeviceInfo(_ *kubernetes.Clientset, _ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	var deviceInfos []api.DeviceInfo
	var gpus []*NvidiaGPU
	for _, slavePod := range slavePods {
		resources, err := m.GetPodGPUResources(slavePod.Name, slavePod.Namespace)
		if err != nil {
			return deviceInfos, err
		}
		gpus = append(gpus, resources...)
	}
	for _, gpu := range gpus {
		deviceInfos = append(deviceInfos, api.DeviceInfo{
			DeviceID:       gpu.UUID,
			DeviceFilePath: gpu.DeviceFilePath,
			Rule: devices.Rule{
				Type:        devices.CharDevice,
				Major:       DEFAULT_NVIDIA_MAJOR_NUMBER,
				Minor:       int64(gpu.MinorNumber),
				Permissions: DEFAULT_CGROUP_PERMISSION,
				Allow:       false,
			},
		})
	}
	return deviceInfos, nil
}

func (m *NvidiaGPUMounter) GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
	if err := m.UpdateGPUStatus(); err != nil {
		return nil, err
	}
	var processInfos []nvml.ProcessInfo
	for _, gpuInfo := range deviceInfos {
		if gpuInfo.DeviceID == "" {
			continue
		}
		for _, gpu := range m.GPUList {
			if gpuInfo.DeviceID != gpu.UUID {
				continue
			}
			procs, err := gpu.GetRunningProcess()
			if err != nil {
				break
			}
			processInfos = append(processInfos, procs...)
			break
		}
	}
	processes := sets.NewInt()
	ctrPids := sets.NewInt(containerPids...)
	for _, info := range processInfos {
		if ctrPids.Has(int(info.Pid)) {
			processes.Insert(int(info.Pid))
		}
	}
	return processes.List(), nil
}

// 卸载设备成功前的后续动作
func (m *NvidiaGPUMounter) UnMountDeviceInfoAfter(_ *kubernetes.Clientset, _ util.Config, _ *v1.Pod, _ *api.Container, _ []*v1.Pod) error {
	return nil
}

func (m *NvidiaGPUMounter) CleanupPodResources(_ *kubernetes.Clientset,
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
