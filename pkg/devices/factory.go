package devices

import (
	ascend_npu "k8s-device-mounter/pkg/devices/ascend/npu"
	nvidia_gpu "k8s-device-mounter/pkg/devices/nvidia/gpu"
	volcano_vgpu "k8s-device-mounter/pkg/devices/volcano/vgpu"
	"k8s-device-mounter/pkg/framework"
)

// 检验是否实现接口
var _ framework.DeviceMounter = &nvidia_gpu.NvidiaGPUMounter{}
var _ framework.DeviceMounter = &volcano_vgpu.VolcanoVGPUMounter{}
var _ framework.DeviceMounter = &ascend_npu.AscendNPUMounter{}

func init() {
	framework.AddDeviceMounterFuncs(nvidia_gpu.NewNvidiaGPUMounter)
	framework.AddDeviceMounterFuncs(volcano_vgpu.NewVolcanoVGPUMounter)
	framework.AddDeviceMounterFuncs(ascend_npu.NewAscendNPUMounter)
}
