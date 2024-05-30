package devices

import (
	"k8s-device-mounter/pkg/api"
	nvidiagpu "k8s-device-mounter/pkg/devices/nvidia/gpu"
	volcanovgpu "k8s-device-mounter/pkg/devices/volcano/vgpu"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type DeviceMounterInterface interface {
	// 描述设备挂载器的类型
	DeviceType() string
	// 检查节点设备环境 如环境不允许则不启动挂载器
	CheckDeviceEnvironment() bool

	// 校验挂载资源时的 请求参数 和 节点资源
	CheckMountResources(kubeClient *kubernetes.Clientset, node *v1.Node, ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string) (api.ResultCode, string, bool)
	// 构建要创建的奴隶pod模板
	BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string, slavePods []*v1.Pod) ([]*v1.Pod, error)
	// 校验从属pod状态是否成功
	CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error)

	// 获取挂载的设备信息
	GetMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 挂载设备后的 后续动作
	MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, config *util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error

	// 获取卸载的设备信息
	GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 获取在设备上运行的容器进程id
	GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error)
}

// 检验是否实现接口
var _ DeviceMounterInterface = &nvidiagpu.NvidiaGPUMounter{}
var _ DeviceMounterInterface = &volcanovgpu.VolcanoVGPUMounter{}

var RegisterDeviceMounter = make(map[string]DeviceMounterInterface)

// TODO 在这里注册设备挂载器
func RegisrtyDeviceMounter() error {

	if gpuMounter, err := nvidiagpu.NewNvidiaGPUMounter(); err != nil {
		klog.Errorf(err.Error())
	} else {
		RegisterDeviceMounter[gpuMounter.DeviceType()] = gpuMounter
	}

	if vgpuMounter, err := volcanovgpu.NewVolcanoVGPUMounter(); err != nil {
		klog.Errorf(err.Error())
	} else {
		RegisterDeviceMounter[vgpuMounter.DeviceType()] = vgpuMounter
	}

	return nil
}
