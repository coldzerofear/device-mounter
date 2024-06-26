package npu

import (
	"strings"

	"Ascend-device-plugin/pkg/common"
	"k8s-device-mounter/pkg/api"
	v1 "k8s.io/api/core/v1"
)

const (
	AscendRtVisibleDevicesEnv = "ASCEND_RT_VISIBLE_DEVICES"

	// AscendVisibleDevicesEnv visible devices env
	// 使用ASCEND_VISIBLE_DEVICES环境变量指定被挂载至容器中的NPU设备，
	// 使用设备序号指定设备，支持单个和范围指定且支持混用。
	AscendVisibleDevicesEnv = "ASCEND_VISIBLE_DEVICES"

	// 对参数ASCEND_VISIBLE_DEVICES中指定的芯片ID作出限制：
	// NODRV：表示不挂载驱动相关目录。
	// VIRTUAL：表示挂载的是虚拟芯片。
	// NODRV,VIRTUAL：表示挂载的是虚拟芯片，并且不挂载驱动相关目录。
	AscendRuntimeOptionsEnv = "ASCEND_RUNTIME_OPTIONS"
	// 读取配置文件中的挂载内容。
	AscendRuntimeMountsEnv = "ASCEND_RUNTIME_MOUNTS"
	// 从物理NPU设备中切分出一定数量的AI Core，指定为虚拟设备。
	// 支持的取值为（vir01，vir02，vir04，vir08，vir16）。
	//Ascend 710系列处理器仅支持vir01、vir02和vir04。
	//Ascend 910系列处理器仅支持vir02、vir04、vir08和vir16。
	//需配合参数“ASCEND_VISIBLE_DEVICES”一起使用，
	//参数“ASCEND_VISIBLE_DEVICES”指定用于切分的物理NPU设备。
	AscendVNPUSpecsEnv = "ASCEND_VNPU_SPECS"

	ASCEND_DEVICE_FILE_PREFIX   = "/dev/davinci"
	ASCEND_DAVINCI_MANAGER_PATH = "/dev/davinci_manager"
	ASCEND_DEVMM_SVM_FILE_PATH  = "/dev/devmm_svm"
	ASCEND_HISI_HDC_FILE_PATH   = "/dev/hisi_hdc"
	ASCEND_VDEVICE_FILE_PREFIX  = "/dev/vdavinci"

	DEFAULT_DAVINCI_MAJOR_NUMBER = 236
	// TODO vNPU设备id取值范围 https://www.hiascend.com/document/detail/zh/computepoweralloca/300/cpaug/cpaug/cpaug_00010.html
	//Ascend 310P的vnpu_id的取值范围为[phy_id*16 + 100, phy_id * 16+107]。
	//Ascend 910的vnpu_id的取值范围为[phy_id*16 + 100, phy_id * 16+115]。
	VNPU_DEVICE_INDEX_START = 100

	DEFAULT_CGROUP_PERMISSION = "rwm"
)

func IsVirtDev(devId int) bool {
	return devId >= VNPU_DEVICE_INDEX_START
}

func HasNPU(pod *v1.Pod, container *api.Container) bool {
	c := pod.Spec.Containers[container.Index]
	for name, _ := range c.Resources.Limits {
		if strings.HasPrefix(string(name), common.ResourceNamePrefix) {
			return true
		}
	}
	return false
}
