package framework

import (
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type DeviceMounter interface {
	// 描述设备挂载器的类型
	DeviceType() string

	// 校验挂载资源时的 请求参数 和 节点资源
	CheckMountResources(kubeClient *kubernetes.Clientset, node *v1.Node, ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string) (api.ResultCode, string, bool)
	// 构建要创建的奴隶pod模板
	BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string, oldSlavePods []*v1.Pod) ([]*v1.Pod, error)
	// 校验从属pod状态是否成功
	CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error)

	// 获取挂载的设备信息
	GetMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 挂载设备成功前的后续动作
	MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, config *util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error

	// 获取卸载的设备信息
	GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 获取在设备上运行的容器进程id
	GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error)
	// 卸载设备成功前的后续动作
	UnMountDeviceInfoAfter(kubeClient *kubernetes.Clientset, config *util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error
	// 返回卸载设备时要被回收的pod资源
	RecycledPodResources(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) []types.NamespacedName
}

var (
	RegisterDeviceMounter = make(map[string]DeviceMounter)
	AddDeviceMounterFuncs []func() (DeviceMounter, error)
)

// TODO 在这里注册设备挂载器
func RegisrtyDeviceMounter() error {
	for _, f := range AddDeviceMounterFuncs {
		mounter, err := f()
		if err != nil {
			klog.Errorf(err.Error())
			continue
		}
		RegisterDeviceMounter[mounter.DeviceType()] = mounter
	}
	return nil
}
