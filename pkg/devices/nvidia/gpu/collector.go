package gpu

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/coldzerofear/device-mounter/pkg/client"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/podresources/v1alpha1"
)

type GPUCollector struct {
	sync.Mutex
	GPUList []*NvidiaGPU
}

func NewGPUCollector() (*GPUCollector, error) {
	gpuCollector := &GPUCollector{}
	if err := gpuCollector.initGPUInfo(); err != nil {
		klog.Errorf("Failed to init gpu info: %v", err)
		return nil, err
	}
	// 更新gpu信息、所属信息
	if err := gpuCollector.UpdateGPUStatus(); err != nil {
		klog.Errorf("Failed to update gpu status: %v", err)
		return nil, err
	}
	klog.Infoln("Successfully update gpu status")
	return gpuCollector, nil
}

func (gpuCollector *GPUCollector) initGPUInfo() error {
	klog.V(4).Infoln("init gpu info")
	if rt := nvml.Init(); rt != nvml.SUCCESS {
		return fmt.Errorf("nvml Init error: %s", nvml.ErrorString(rt))
	}
	defer nvml.Shutdown()

	num, rt := nvml.DeviceGetCount()
	if rt != nvml.SUCCESS {

		return fmt.Errorf("nvml DeviceGetCount error: %s", nvml.ErrorString(rt))
	} else {
		klog.V(4).Infoln("GPU Num: ", num)
	}

	for i := 0; i < num; i++ {
		dev, rt := nvml.DeviceGetHandleByIndex(i)
		if rt != nvml.SUCCESS {
			return fmt.Errorf("nvml DeviceGetHandleByIndex error: %s", nvml.ErrorString(rt))
		}
		minorNum, rt := dev.GetMinorNumber()
		if rt != nvml.SUCCESS {
			return fmt.Errorf("nvml DeviceGetMinorNumber error: %s", nvml.ErrorString(rt))
		}
		uuid, rt := dev.GetUUID()
		if rt != nvml.SUCCESS {
			return fmt.Errorf("nvml DeviceGetUUID error: %s", nvml.ErrorString(rt))
		}
		gpuDev := New(minorNum, uuid)
		gpuCollector.GPUList = append(gpuCollector.GPUList, gpuDev)
	}
	return nil
}

func (gpuCollector *GPUCollector) GetPodGPUResources(podName, podNamespace string) ([]*NvidiaGPU, error) {
	if err := gpuCollector.UpdateGPUStatus(); err != nil {
		klog.Errorln("Failed to update gpu status")
		return nil, err
	}
	var gpuResources []*NvidiaGPU
	for _, gpuDev := range gpuCollector.GPUList {
		if gpuDev.PodName == podName && gpuDev.PodNamespace == podNamespace {
			gpuResources = append(gpuResources, gpuDev)
		}
	}
	return gpuResources, nil
}

func (gpuCollector *GPUCollector) GetContainerGPUResources(podName, podNamespace, containerName string) ([]*NvidiaGPU, error) {
	if err := gpuCollector.UpdateGPUStatus(); err != nil {
		klog.Errorln("Failed to update gpu status")
		return nil, err
	}
	var gpuResources []*NvidiaGPU
	for _, gpuDev := range gpuCollector.GPUList {
		if gpuDev.PodName == podName && gpuDev.PodNamespace == podNamespace &&
			gpuDev.ContainerName == containerName {
			gpuResources = append(gpuResources, gpuDev)
		}
	}
	return gpuResources, nil
}

func (gpuCollector *GPUCollector) UpdateGPUStatus() error {
	gpuCollector.Lock()
	defer gpuCollector.Unlock()

	klog.V(4).Infoln("Updating GPU status")

	resClient := client.GetPodResourcesClinet().GetClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := resClient.List(ctx, &v1alpha1.ListPodResourcesRequest{})
	if err != nil {
		return err
	}
	// 重置gpu状态
	gpuCollector.resetGPUStatus()

	// 搜索哪些pod分配到了哪些gpu设备
	for _, pod := range resp.GetPodResources() {
		for _, container := range pod.GetContainers() {
			for _, dev := range container.GetDevices() {
				if dev.GetResourceName() != ResourceName {
					continue
				}

				for _, uuid := range dev.GetDeviceIds() { // nvidia-device-plugin 上报的是gpu的uuid
					if nvidiaGPU, err := gpuCollector.GetGPUByUUID(uuid); err != nil {
						klog.V(4).Infoln(err.Error())
						// TODO 发现新的设备
						minor, err := SearchGPUMinorByUUID(uuid)
						if err != nil {
							klog.Errorf(err.Error())
							continue
						}
						newGPU := New(minor, uuid)
						newGPU.State = GPU_ALLOCATED_STATE
						newGPU.PodName = pod.Name
						newGPU.PodNamespace = pod.Namespace
						newGPU.ContainerName = container.Name
						gpuCollector.GPUList = append(gpuCollector.GPUList, newGPU)
					} else {
						// 更新 gpu 信息
						nvidiaGPU.State = GPU_ALLOCATED_STATE
						nvidiaGPU.PodName = pod.Name
						nvidiaGPU.PodNamespace = pod.Namespace
						nvidiaGPU.ContainerName = container.Name
						klog.V(4).InfoS("GPU allocated", "ID", nvidiaGPU.UUID,
							"Device", nvidiaGPU.DeviceFilePath, "PodName", pod.Name, "Namespace",
							pod.Namespace, "ContainerName", container.Name)
					}
				}
			}
		}
	}
	klog.V(4).Infoln("GPU status update successfully")
	return nil
}

func SearchGPUMinorByUUID(uuid string) (int, error) {
	if rt := nvml.Init(); rt != nvml.SUCCESS {

		return 0, fmt.Errorf("nvml Init error: %s", nvml.ErrorString(rt))
	}
	defer nvml.Shutdown()
	handle, rt := nvml.DeviceGetHandleByUUID(uuid)
	if rt != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml DeviceGetHandleByUUID error: %s", nvml.ErrorString(rt))
	}
	number, rt := handle.GetMinorNumber()
	if rt != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml DeviceGetMinorNumber error: %s", nvml.ErrorString(rt))
	}
	return number, nil
}

func (gpuCollector *GPUCollector) GetGPUByUUID(uuid string) (*NvidiaGPU, error) {
	for _, gpuDev := range gpuCollector.GPUList {
		if gpuDev.UUID == uuid {
			return gpuDev, nil
		}
	}
	return nil, fmt.Errorf("No GPU with UUID " + uuid)
}

func (gpuCollector *GPUCollector) resetGPUStatus() {
	for _, gpuDev := range gpuCollector.GPUList {
		gpuDev.ResetState()
	}
}

type NvidiaGPU struct {
	MinorNumber    int
	DeviceFilePath string
	UUID           string
	State          GPUState
	PodName        string
	PodNamespace   string
	ContainerName  string
}

type GPUState string

const (
	GPU_FREE_STATE      GPUState = "GPU_FREE_STATE"
	GPU_ALLOCATED_STATE GPUState = "GPU_ALLOCATED_STATE"
)

func New(minorNumber int, uuid string) *NvidiaGPU {
	return &NvidiaGPU{
		MinorNumber:    minorNumber,
		DeviceFilePath: NVIDIA_DEVICE_FILE_PREFIX + strconv.Itoa(minorNumber),
		UUID:           uuid,
		State:          GPU_FREE_STATE,
		PodName:        "",
		PodNamespace:   "",
		ContainerName:  "",
	}
}

func (gpu *NvidiaGPU) String() string {
	out, err := json.Marshal(gpu)
	if err != nil {
		klog.Errorln("Failed to parse gpu object to json")
		return "Failed to parse gpu object to json"
	}
	return string(out)
}

func (gpu *NvidiaGPU) ResetState() {
	gpu.PodName = ""
	gpu.PodNamespace = ""
	gpu.ContainerName = ""
	gpu.State = GPU_FREE_STATE
}

func (gpu *NvidiaGPU) GetRunningProcess() ([]nvml.ProcessInfo, error) {
	if rt := nvml.Init(); rt != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml Init error: %s", nvml.ErrorString(rt))
	}
	defer nvml.Shutdown()
	handle, rt := nvml.DeviceGetHandleByUUID(gpu.UUID)
	if rt != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml DeviceGetHandleByUUID error: %s", nvml.ErrorString(rt))
	}
	graphicsProcesses, rt := handle.GetGraphicsRunningProcesses()
	if rt != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml DeviceGetGraphicsRunningProcesses error: %s", nvml.ErrorString(rt))
	}
	computeProcesses, rt := handle.GetComputeRunningProcesses()
	if rt != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml DeviceGetComputeRunningProcesses error: %s", nvml.ErrorString(rt))
	}
	return append(graphicsProcesses, computeProcesses...), nil
}
