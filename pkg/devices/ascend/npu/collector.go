package npu

import (
	"context"
	"strings"
	"sync"
	"time"

	"Ascend-device-plugin/pkg/common"
	"huawei.com/npu-exporter/v6/devmanager"
	npuCommon "huawei.com/npu-exporter/v6/devmanager/common"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/podresources/v1alpha1"
)

type NPUCollector struct {
	sync.Mutex
	*devmanager.DeviceManager
}

func (c *NPUCollector) GetPodNPUResourcesFunc(f func(*v1alpha1.PodResources)) error {
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
		if resources != nil {
			f(resources)
		}
	}
	return nil
}

func (c *NPUCollector) GetSlavePodsDeviceInfo(slavePods []*v1.Pod, f func(devId int) (api.DeviceInfo, error)) ([]api.DeviceInfo, error) {
	var containerDevices []*v1alpha1.ContainerDevices
	if err := c.GetPodNPUResourcesFunc(func(resources *v1alpha1.PodResources) {
		for _, pod := range slavePods {
			if resources.Name == pod.Name && resources.Namespace == pod.Namespace {
				for _, containerResources := range resources.GetContainers() {
					if len(containerResources.GetDevices()) == 0 {
						continue
					}
					containerDevices = append(containerDevices, containerResources.GetDevices()...)
				}
			}
		}
	}); err != nil {
		return nil, err
	}
	var visibleDevices []int
	for _, device := range containerDevices {
		if !strings.HasPrefix(device.ResourceName, ResourceNamePrefix) {
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
