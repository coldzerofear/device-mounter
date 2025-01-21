package npu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"Ascend-device-plugin/pkg/common"
	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/client"
	"huawei.com/npu-exporter/v6/devmanager"
	npuCommon "huawei.com/npu-exporter/v6/devmanager/common"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/podresources/v1alpha1"
)

type NPUCollector struct {
	sync.Mutex
	*devmanager.DeviceManager
}

func (c *NPUCollector) GetPodNPUResourcesFunc(matchFunc func(*v1alpha1.PodResources) (int, bool), f func(*v1alpha1.PodResources, int) error) error {
	c.Lock()
	defer c.Unlock()
	resClient := client.GetPodResourcesClinet().GetClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := resClient.List(ctx, &v1alpha1.ListPodResourcesRequest{})
	if err != nil {
		return err
	}
	for _, resources := range resp.GetPodResources() {
		if resources == nil {
			continue
		}
		if idx, ok := matchFunc(resources); ok {
			if err = f(resources, idx); err != nil {
				return err
			}
		}
	}
	return nil
}

func GetRealDeviceForAnnotations(ctx context.Context, kubeClient *kubernetes.Clientset, pod *v1.Pod) []string {
	kltDevStr, ok1 := pod.Annotations[common.ResourceNamePrefix+common.Pod2kl]
	realDevStr, ok2 := pod.Annotations[common.ResourceNamePrefix+common.PodRealAlloc]
	if !ok1 || !ok2 {
		newPod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			newPod = &v1.Pod{}
			newPod.Annotations = map[string]string{}
		}
		kltDevStr, ok1 = pod.Annotations[common.ResourceNamePrefix+common.Pod2kl]
		realDevStr, ok2 = newPod.Annotations[common.ResourceNamePrefix+common.PodRealAlloc]
	}
	if ok1 && ok2 && kltDevStr != realDevStr {
		return strings.Split(realDevStr, common.CommaSepDev)
	}
	return nil
}

func (c *NPUCollector) GetSlavePodsDeviceInfo(ctx context.Context, kubeClient *kubernetes.Clientset, slavePods []*v1.Pod, f func(devId int) (api.DeviceInfo, error)) ([]api.DeviceInfo, error) {
	var containerDevices []*v1alpha1.ContainerDevices

	matchFunc := func(res *v1alpha1.PodResources) (int, bool) {
		for i, pod := range slavePods {
			if res.Name == pod.Name && res.Namespace == pod.Namespace {
				return i, true
			}
		}
		return 0, false
	}
	loadFunc := func(resources *v1alpha1.PodResources, idx int) error {
		pod := slavePods[idx]
		klog.Infoln("Current matched npu slave pod", "name", pod.Name, "namespace", pod.Namespace)
		// TODO 通过注解得知真实分配的设备
		realDevs := GetRealDeviceForAnnotations(ctx, kubeClient, pod)
		if len(realDevs) > 0 {
			containerDevices = append(containerDevices, &v1alpha1.ContainerDevices{
				ResourceName: common.ResourceNamePrefix,
				DeviceIds:    realDevs,
			})
			klog.Infof("The current pod [%s] obtains the actual allocated NPU devices from the annotation [%s]",
				fmt.Sprintf("%s/%s", pod.Namespace, pod.Name), common.ResourceNamePrefix+common.PodRealAlloc)
			return nil
		}
		for _, containerResources := range resources.GetContainers() {
			// 过滤掉SlavePod可能存在的无用容器：例如：外部组件插入的sidecar
			if len(containerResources.GetDevices()) == 0 {
				continue
			}
			var ctrDevices []*v1alpha1.ContainerDevices

			// TODO 从volcano获得分配的设备
			for _, dev := range containerResources.GetDevices() {
				// 过滤掉非Ascend NPU设备
				if !strings.HasPrefix(dev.GetResourceName(), common.ResourceNamePrefix) {
					continue
				}

				// 从volcano注解处获取更多信息
				resSplit := strings.Split(dev.GetResourceName(), "/")
				annotation, err := common.GetPodAnnotationByDeviceType(pod, resSplit[1])
				if err != nil {
					klog.Errorln("GetPodAnnotationByDeviceType failed", "name", pod.Name,
						"namespace", pod.Namespace, "resourceName", dev.GetResourceName(), err.Error())
					//klog.Infoln("No NPU scheduling annotation found for Volcano")
					continue
				}

				if strings.Contains(dev.GetResourceName(), common.AiCoreResourceName) {
					// 动态vNPU设备分配
					// example: huawei.com/npu-core:0-vir02、huawei.com/npu-core:0-vir04
					deviceInfos := strings.Split(annotation, common.MiddelLine)
					if len(deviceInfos) > 1 {
						realDevs = GetRealDeviceForAnnotations(ctx, kubeClient, pod)
						if len(realDevs) > 0 {
							containerDevices = append(containerDevices, &v1alpha1.ContainerDevices{
								ResourceName: common.ResourceNamePrefix,
								DeviceIds:    realDevs,
							})
							klog.Infof("The current pod [%s] obtains the actual allocated NPU devices from the annotation [%s]",
								fmt.Sprintf("%s/%s", pod.Namespace, pod.Name), common.ResourceNamePrefix+common.PodRealAlloc)
							return nil
						}
						// TODO 此时已经无法得知实际分配的设备
						return fmt.Errorf("Unable to determine the actual device allocated by Volcano")
					}
					// example: huawei.com/npu-core:0,1,2,3
					ids := strings.Split(deviceInfos[0], common.CommaSepDev)
					var phyDevs []string
					for _, id := range ids {
						devType := convertDevType(c.GetDevType())
						phyDevs = append(phyDevs, fmt.Sprintf("%s-%s", devType, id))
					}
					ctrDevices = []*v1alpha1.ContainerDevices{{
						ResourceName: dev.GetResourceName(),
						DeviceIds:    phyDevs,
					}}
				} else {
					// 静态设备分配
					ctrDevices = []*v1alpha1.ContainerDevices{{
						ResourceName: dev.GetResourceName(),
						DeviceIds:    strings.Split(annotation, common.CommaSepDev),
					}}
				}
				break
			}

			// TODO 通过pod resources得知分配的设备
			if len(ctrDevices) == 0 {
				ctrDevices = containerResources.GetDevices()
				klog.Infof("The current pod [%s] obtains the actual allocated NPU devices from the pod resources",
					fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
			}

			containerDevices = append(containerDevices, ctrDevices...)
			// 退出循环
			break
		}
		return nil
	}
	if err := c.GetPodNPUResourcesFunc(matchFunc, loadFunc); err != nil {
		return nil, err
	}
	var visibleDevices []int
	for _, device := range containerDevices {
		if !strings.HasPrefix(device.GetResourceName(), common.ResourceNamePrefix) {
			continue
		}
		ascendRuntimeOptions := ""
		for _, deviceName := range device.GetDeviceIds() {
			if common.IsVirtualDev(deviceName) {
				ascendRuntimeOptions = common.VirtualDev
				break
			}
		}
		_, ascendVisibleDevices, err := common.GetDeviceListID(device.GetDeviceIds(), ascendRuntimeOptions)
		if err != nil {
			return nil, err
		}
		visibleDevices = append(visibleDevices, ascendVisibleDevices...)
	}
	var deviceInfos []api.DeviceInfo
	for _, deviceId := range visibleDevices {
		if f != nil {
			info, err := f(deviceId)
			if err != nil {
				return nil, err
			}
			deviceInfos = append(deviceInfos, info)
		}
	}
	return deviceInfos, nil
}

func (c *NPUCollector) GetRunningProcess() ([]*npuCommon.DevProcessInfo, error) {
	if err := c.Init(); err != nil {
		return nil, err
	}
	defer func() {
		if err := c.ShutDown(); err != nil {
			klog.Errorln("Ascend dcmi shutdown failed: ", err)
		}
	}()
	_, logicIds, err := c.GetDeviceList()
	if err != nil {
		return nil, err
	}
	var processInfos []*npuCommon.DevProcessInfo
	for _, logicId := range logicIds {
		_, err = c.GetDeviceHealth(logicId)
		// 校验是否掉卡
		if common.CheckErrorMessage(err, npuCommon.DeviceNotReadyErrCodeStr) {
			klog.Errorf("logic id %d, error message contains %s, device does not ready, "+
				"the card may be dropped", logicId, npuCommon.DeviceNotReadyErrCodeStr)
			continue
		}
		processInfo, err := c.GetDevProcessInfo(logicId)
		if err != nil {
			return nil, err
		}
		if processInfo != nil {
			processInfos = append(processInfos, processInfo)
		}
	}
	return processInfos, nil
}

func convertDevType(devType string) string {
	switch devType {
	case common.Ascend910, common.Ascend910B, common.Ascend910A3:
		return common.Ascend910
	case common.Ascend310P:
		return common.Ascend310P
	case common.Ascend310, common.Ascend310B:
		return common.Ascend310
	default:
		return ""
	}
}
