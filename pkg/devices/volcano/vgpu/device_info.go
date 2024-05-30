package vgpu

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/opencontainers/runc/libcontainer/devices"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type VolcanoVGPUMounter struct{}

func NewVolcanoVGPUMounter() (*VolcanoVGPUMounter, error) {
	klog.Infoln("Creating VolcanoVGPUMounter")
	mounter := &VolcanoVGPUMounter{}
	if !mounter.CheckDeviceEnvironment() {
		return mounter, fmt.Errorf("The current node environment does not have the operating conditions for VolcanoVGPUMounter")
	}
	klog.Infoln("Successfully created VolcanoVGPUMounter")
	return mounter, nil
}

// 描述设备挂载器的类型
func (m *VolcanoVGPUMounter) DeviceType() string {
	return "VOLCANO_VGPU"
}

// 检查节点设备环境 如环境不允许则不启动挂载器
func (m *VolcanoVGPUMounter) CheckDeviceEnvironment() bool {
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

// 校验挂载资源时的 请求参数 和 节点资源
func (m *VolcanoVGPUMounter) CheckMountResources(
	kubeClient *kubernetes.Clientset,
	node *v1.Node,
	ownerPod *v1.Pod,
	container *api.Container,
	request map[v1.ResourceName]resource.Quantity,
	annotations map[string]string) (api.ResultCode, string, bool) {

	if !util.CheckResourcesInSlice(request, []string{VolcanoVGPUNumber},
		[]string{VolcanoVGPUMemory, VolcanoVGPUCores, VolcanoVGPUMemoryPercentage}) {
		return api.ResultCode_Fail, "Request for resources error", false
	}
	if !util.CheckResourcesInNode(node, map[v1.ResourceName]resource.Quantity{
		VolcanoVGPUNumber: request[VolcanoVGPUNumber],
	}) {
		return api.ResultCode_Insufficient, "Insufficient node resources", false
	}

	expansion := config.AnnoIsExpansion(annotations)

	if str, ok := ownerPod.Annotations[AssignedIDsAnnotations]; ok && expansion {
		podDevices := decodePodDevices(str)
		usedUUID := sets.NewString()
		for _, devices := range podDevices {
			for _, device := range devices {
				if device.CtrIdx == container.Index {
					usedUUID.Insert(device.UUID)
				}
			}
		}
		quantity := request[VolcanoVGPUNumber]
		if quantity.Value() > int64(len(usedUUID)) {
			return api.ResultCode_Fail, "The requested resource count exceeds the target container resource count and cannot be expanded", false
		}
	} else if expansion {
		return api.ResultCode_Fail, "The target container does not have vgpu resources and cannot be expanded", false
	} else {
		if !HasVGPU(ownerPod, container) {
			return api.ResultCode_Fail, "The target pod has not applied for a vgpu and cannot be mounted", false
		}
	}

	return api.ResultCode_Success, "", true
}

// 构建要创建的奴隶pod模板
func (m *VolcanoVGPUMounter) BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string, slavePods []*v1.Pod) ([]*v1.Pod, error) {
	var podDevices []ContainerDevices

	if str, ok := ownerPod.Annotations[AssignedIDsAnnotations]; ok {
		podDevices = append(podDevices, decodePodDevices(str)...)
		if config.AnnoIsExpansion(annotations) {
			// 扩容 要指定slave pod调度到扩容的设备上
			usedUUIDs := getDevicesUUID(podDevices, container)
			annotations[GPUUseUUID] = strings.Join(usedUUIDs, ",")
		} else {
			// 挂载新设备 要排除掉容器原本已分配的设备 和 以往挂载过的设备
			for _, oldSlavePod := range slavePods {
				devs := decodePodDevices(oldSlavePod.Annotations[AssignedIDsAnnotations])
				podDevices = append(podDevices, devs...)
			}
			usedUUIDs := getDevicesUUID(podDevices, container)
			if len(usedUUIDs) > 0 {
				annotations[GPUNoUseUUID] = strings.Join(usedUUIDs, ",")
			}
		}
	}
	// TODO volcano vgpu 不考虑分多个pod申请资源
	pod := util.NewDeviceSlavePod(ownerPod, request, annotations)
	pod.Spec.SchedulerName = "volcano"
	return []*v1.Pod{pod}, nil
}

// 校验从属pod状态是否成功
func (m *VolcanoVGPUMounter) CheckDeviceSlavePodStatus(slavePod *v1.Pod) (api.StatusCode, error) {
	if slavePod.Status.Phase == v1.PodRunning {
		podDevices := decodePodDevices(slavePod.Annotations[AssignedIDsAnnotations])
		if !(len(podDevices) > 0) {
			return api.Fail, fmt.Errorf("vGPU device allocation error")
		}
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
	if slavePod.Status.Conditions[0].Reason == v1.PodReasonUnschedulable {
		return api.Unschedulable, fmt.Errorf(slavePod.Status.Conditions[0].Message)
	}
	return api.Wait, nil
}

// 获取挂载的设备信息
func (m *VolcanoVGPUMounter) GetMountDeviceInfo(_ *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	var deviceInfos []api.DeviceInfo

	if config.AnnoIsExpansion(slavePods[0].Annotations) {
		// TODO 如果是扩容操作 不用配置设备权限
		return deviceInfos, nil
	}

	for _, slavePod := range slavePods {
		podDevices := decodePodDevices(slavePod.Annotations[AssignedIDsAnnotations])
		for _, devs := range podDevices {
			for _, dev := range devs {
				minor, err := GetDeviceMinorByUUID(dev.UUID)
				if err != nil {
					return nil, err
				}
				deviceInfos = append(deviceInfos, api.DeviceInfo{
					DeviceID:       dev.UUID,
					DeviceFilePath: NVIDIA_DEVICE_FILE_PREFIX + strconv.Itoa(minor),
					Rule: devices.Rule{
						Type:        devices.CharDevice,
						Major:       DEFAULT_NVIDIA_MAJOR_NUMBER,
						Minor:       int64(minor),
						Permissions: DEFAULT_CGROUP_PERMISSION,
						Allow:       true,
					},
				})
			}
		}

	}
	// TODO 原始pod上挂载过GPU相关设备文件，跳过下面的步骤
	if HasVGPU(ownerPod, container) {
		return deviceInfos, nil
	}

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
	return deviceInfos, nil
}

func (m *VolcanoVGPUMounter) MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, _ *util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error {
	for _, slavePod := range slavePods {
		devMap := GetPodDevMap(slavePod)
		if config.AnnoIsExpansion(slavePod.Annotations) {
			// 扩容设备的操作
			klog.Infoln("slave", slavePod.Name, "devices ", devMap)
			// 默认调用一次命令，保证vgpu拦截库生成缓存
			_, _, err := client.ExecCmdToPod(kubeClient, ownerPod, container, []string{"nvidia-smi"})
			if err != nil {
				// 这里先忽略失败
				klog.Errorln(err)
			}
			if err = MutationCacheFunc(ownerPod, container, func(cache *sharedRegionT) error {
				for i := uint64(0); i < cache.num; i++ {
					devuuid := string(cache.uuids[i].uuid[:])[0:40]
					if dev, ok := devMap[devuuid]; ok {
						cores := uint64(dev.Usedcores)
						memory := uint64(dev.Usedmem) << 20 // mb转bytes
						klog.Infoln("Expansion device", devuuid, "add memory", memory, "add core", cores)
						cache.limit[i] += memory
						cache.sm_limit[i] += cores
					}
				}
				return nil
			}); err != nil {
				return err
			}
		} else {
			// 挂载设备的操作
			// 检测是否存在vgpu缓存
			_, err := os.Stat(GetVGPUCacheFileDir(ownerPod, container))

			// owner pod 有vgpu资源
			if HasVGPU(ownerPod, container) || err == nil {
				// 默认调用一次命令，保证vgpu拦截库生成缓存
				_, _, err := client.ExecCmdToPod(kubeClient, ownerPod, container, []string{"nvidia-smi"})
				if err != nil {
					// 这里先忽略失败
					klog.Errorln(err)
				}
				if err = MutationCacheFunc(ownerPod, container, func(cache *sharedRegionT) error {
					for devuuid, dev := range devMap {
						tail := cache.num
						cores := uint64(dev.Usedcores)
						memory := uint64(dev.Usedmem) << 20 // mb转bytes
						klog.Infoln("Attach new device", devuuid, "memory limit", memory, "core limit", cores)
						cache.uuids[tail] = ConvertUUID(devuuid)
						cache.limit[tail] = memory
						cache.sm_limit[tail] = cores
						cache.num++
					}
					return nil
				}); err != nil {
					return err
				}
			} else {
				// owner pod 无vgpu资源

				// 校验vgpu库文件
				if _, err := os.Stat(VGPU_LIBFILE_PATH); err != nil {
					return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_LIBFILE_PATH, err)
				}
				if _, err := os.Stat(VGPU_PRELOAD_PATH); err != nil {
					return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_PRELOAD_PATH, err)
				}

				// 复制vgpu库到目标容器
				if _, _, err := client.CopyToPod(kubeClient, ownerPod, container,
					VGPU_LIBFILE_PATH, VGPU_LIBFILE_PATH); err != nil {
					return fmt.Errorf("Copying file [%s] to pod failed: %v", VGPU_LIBFILE_PATH, err)
				}
				if _, _, err := client.CopyToPod(kubeClient, ownerPod, container,
					VGPU_PRELOAD_PATH, "/etc/ld.so.preload"); err != nil {
					return fmt.Errorf("Copying file [%s] to pod failed: %v", VGPU_PRELOAD_PATH, err)
				}
				var uuids []string
				for _, dev := range devMap {
					uuids = append(uuids, dev.UUID)
				}
				cmd := []string{"sh", "-c", "export "}
				if _, _, err := client.ExecCmdToPod(kubeClient, ownerPod, container, cmd); err != nil {
					// 这里先忽略失败
					klog.Errorln(err)
				}
			}
		}
	}
	return nil
}

// 获取卸载的设备信息
func (m *VolcanoVGPUMounter) GetUnMountDeviceInfo(kubeClient *kubernetes.Clientset, _ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
	var deviceInfos []api.DeviceInfo
	for _, slavePod := range slavePods {

		if config.AnnoIsExpansion(slavePod.Annotations) {
			continue // 跳过用于扩容的pod
		}
		podDevices := decodePodDevices(slavePod.Annotations[AssignedIDsAnnotations])
		for _, devs := range podDevices {
			for _, dev := range devs {
				minor, err := GetDeviceMinorByUUID(dev.UUID)
				if err != nil {
					return nil, err
				}
				deviceInfos = append(deviceInfos, api.DeviceInfo{
					DeviceID:       dev.UUID,
					DeviceFilePath: NVIDIA_DEVICE_FILE_PREFIX + strconv.Itoa(minor),
					Rule: devices.Rule{
						Type:        devices.CharDevice,
						Major:       DEFAULT_NVIDIA_MAJOR_NUMBER,
						Minor:       int64(minor),
						Permissions: DEFAULT_CGROUP_PERMISSION,
						Allow:       false,
					},
				})
			}
		}
	}
	return deviceInfos, nil
}

// 获取在设备上运行的容器进程id
func (m *VolcanoVGPUMounter) GetDeviceRunningProcesses(containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
	var pids []int
	for _, info := range deviceInfos {
		if err := DeviceRunningProcessFunc(info.DeviceID, func(process nvml.ProcessInfo) {
			if util.ContainsInt(containerPids, int(process.Pid)) {
				pids = append(pids, int(process.Pid))
			}
		}); err != nil {
			return nil, err
		}
	}
	return pids, nil
}
