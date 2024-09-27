package framework

import (
	"context"
	"strings"
	"sync"

	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type DeviceMounter interface {
	// 获取设备的类型标识
	GetDeviceType() string

	// 验证挂载请求是否有效
	ValidateMountRequest(ctx context.Context, kubeClient *kubernetes.Clientset, node *v1.Node, pod *v1.Pod, container *api.Container, resources map[v1.ResourceName]resource.Quantity, annotations, labels map[string]string) error

	// 构建辅助Pod模板
	BuildSupportPodTemplates(ctx context.Context, pod *v1.Pod, container *api.Container, resources map[v1.ResourceName]resource.Quantity, annotations, labels map[string]string, existingSupportPods []*v1.Pod) ([]*v1.Pod, error)

	// 验证辅助Pod的状态是否正确
	VerifySupportPodStatus(ctx context.Context, supportPod *v1.Pod) (api.StatusCode, error)

	// 获取待挂载设备的信息
	GetDeviceInfosToMount(ctx context.Context, kubeClient *kubernetes.Clientset, pod *v1.Pod, container *api.Container, supportPods []*v1.Pod) ([]api.DeviceInfo, error)

	// 执行挂载设备后的操作
	ExecutePostMountActions(ctx context.Context, kubeClient *kubernetes.Clientset, config util.Config, pod *v1.Pod, container *api.Container, supportPods []*v1.Pod) error

	// 获取待卸载设备的信息
	GetDeviceInfosToUnmount(ctx context.Context, kubeClient *kubernetes.Clientset, pod *v1.Pod, container *api.Container, supportPods []*v1.Pod) ([]api.DeviceInfo, error)

	// 获取设备上的活动进程ID
	GetDevicesActiveProcessIDs(ctx context.Context, containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error)

	// 执行卸载设备后的操作
	ExecutePostUnmountActions(ctx context.Context, kubeClient *kubernetes.Clientset, config util.Config, pod *v1.Pod, container *api.Container, supportPods []*v1.Pod) error

	// 获取卸载设备后需要清理的Pod资源
	GetPodsToCleanup(ctx context.Context, kubeClient *kubernetes.Clientset, pod *v1.Pod, container *api.Container, supportPods []*v1.Pod) []api.ObjectKey
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
		devType := strings.ToUpper(mounter.GetDeviceType())
		registerDeviceMounter[devType] = mounter
	}
	return nil
}

func GetDeviceMounterTypes() []string {
	lock.Lock()
	defer lock.Unlock()
	var deviceTypes []string
	for _, mounter := range registerDeviceMounter {
		deviceTypes = append(deviceTypes, mounter.GetDeviceType())
	}
	return deviceTypes
}

func GetDeviceMounter(devType string) (DeviceMounter, bool) {
	lock.Lock()
	mounter, ok := registerDeviceMounter[devType]
	lock.Unlock()
	return mounter, ok
}
