package framework

import (
	"strings"
	"sync"

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
	CheckMountResources(kubeClient *kubernetes.Clientset, node *v1.Node, ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations, labels map[string]string) (api.ResultCode, string, bool)
	// 构建要创建的奴隶pod模板
	BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations, labels map[string]string, oldSlavePods []*v1.Pod) ([]*v1.Pod, error)
	// 校验从属pod状态是否成功
	CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error)

	// 获取挂载的设备信息
	GetMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 挂载设备成功前的后续动作
	MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, config util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error

	// 获取卸载的设备信息
	GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error)
	// 获取在设备上运行的容器进程id
	GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error)
	// 卸载设备成功前的后续动作
	UnMountDeviceInfoAfter(kubeClient *kubernetes.Clientset, config util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error
	// 返回卸载设备时要被清理回收的pod资源
	CleanupPodResources(kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) []types.NamespacedName
}

type CreateMounterFunc func() (DeviceMounter, error)

var (
	lock                  sync.Mutex
	registerDeviceMounter map[string]DeviceMounter
	addDeviceMounterFuncs []CreateMounterFunc
)

func init() {
	registerDeviceMounter = make(map[string]DeviceMounter)
	addDeviceMounterFuncs = make([]CreateMounterFunc, 0)
}

func AddDeviceMounterFuncs(createFunc CreateMounterFunc) {
	lock.Lock()
	if createFunc != nil {
		addDeviceMounterFuncs = append(addDeviceMounterFuncs, createFunc)
	}
	lock.Unlock()
}

// TODO 在这里注册设备挂载器
func RegisrtyDeviceMounter() error {
	lock.Lock()
	defer lock.Unlock()
	for _, createFunc := range addDeviceMounterFuncs {
		mounter, err := createFunc()
		if err != nil {
			klog.Errorf(err.Error())
			continue
		}
		devType := strings.ToUpper(mounter.DeviceType())
		registerDeviceMounter[devType] = mounter
	}
	return nil
}

func GetDeviceMounterTypes() []string {
	lock.Lock()
	defer lock.Unlock()
	var deviceTypes []string
	for _, mounter := range registerDeviceMounter {
		deviceTypes = append(deviceTypes, mounter.DeviceType())
	}
	return deviceTypes
}

func GetDeviceMounter(devType string) (DeviceMounter, bool) {
	lock.Lock()
	mounter, ok := registerDeviceMounter[devType]
	lock.Unlock()
	return mounter, ok
}
