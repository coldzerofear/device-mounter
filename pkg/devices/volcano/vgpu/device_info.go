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
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type VolcanoVGPUMounter struct{}

func NewVolcanoVGPUMounter() (framework.DeviceMounter, error) {
	klog.Infoln("Creating VolcanoVGPUMounter")
	if !checkDeviceEnvironment() {
		return nil, fmt.Errorf("The current node environment does not have the operating conditions for VolcanoVGPUMounter")
	}
	mounter := &VolcanoVGPUMounter{}
	klog.Infoln("Successfully created VolcanoVGPUMounter")
	return mounter, nil
}

// 描述设备挂载器的类型
func (m *VolcanoVGPUMounter) DeviceType() string {
	return "VOLCANO_VGPU"
}

// 检查节点设备环境 如环境不允许则不启动挂载器
func checkDeviceEnvironment() bool {
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
	_ *kubernetes.Clientset,
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
	devs, hasVGPU := ownerPod.Annotations[AssignedIDsAnnotations]
	switch {
	case hasVGPU && expansion:
		podDevices := decodePodDevices(devs)
		usedUUIDs := getDevicesUUID(podDevices, container)
		quantity := request[VolcanoVGPUNumber]
		if quantity.Value() > int64(len(usedUUIDs)) {
			return api.ResultCode_Fail, "The requested resource count exceeds the target container resource count and cannot be expanded", false
		}
	case expansion:
		if _, err := os.Stat(GetVGPUCacheFileDir(ownerPod, container)); err != nil {
			return api.ResultCode_Fail, "The target container does not have vgpu resources and cannot be expanded", false
		}
	default:
		// 非扩展请求校验容器是否初始化过vGPU设备
		names := ownerPod.Annotations[InitVGPUAnnotations]
		ctrNames := strings.Split(strings.TrimSpace(names), ",")
		if util.ContainsString(ctrNames, container.Name) {
			return api.ResultCode_Fail, "The target container has initialized the vgpu and cannot be mounted again", false
		}
	}

	return api.ResultCode_Success, "", true
}

// 构建要创建的奴隶pod模板
func (m *VolcanoVGPUMounter) BuildDeviceSlavePodTemplates(ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity, annotations map[string]string, slavePods []*v1.Pod) ([]*v1.Pod, error) {
	var podDevices []ContainerDevices
	expansion := config.AnnoIsExpansion(annotations)
	str, hasVGPU := ownerPod.Annotations[AssignedIDsAnnotations]
	podDevices = append(podDevices, decodePodDevices(str)...)

	switch {
	case expansion && hasVGPU:
		// TODO 为请求了vGPU的容器扩容, 确保slave pod调度到指定的设备上
		usedUUIDs := getDevicesUUID(podDevices, container)
		annotations[GPUUseUUID] = strings.Join(usedUUIDs, ",")
	case expansion:
		// TODO 为热挂载的vGPU扩容， 确保slave pod调度到热挂的vGPU设备上
		for _, slavePod := range slavePods {
			annos := slavePod.Annotations[AssignedIDsAnnotations]
			podDevices = append(podDevices, decodePodDevices(annos)...)
		}
		usedUUIDs := getDevicesUUID(podDevices, &api.Container{Index: 0})
		if len(usedUUIDs) == 0 {
			return nil, fmt.Errorf("Unable to find scalable vGPU devices")
		}
		quantity := request[VolcanoVGPUNumber]
		if quantity.Value() > int64(len(usedUUIDs)) {
			return nil, fmt.Errorf("The number of requested devices [%s] exceeds the number of expandable devices", VolcanoVGPUNumber)
		}
		annotations[GPUUseUUID] = strings.Join(usedUUIDs, ",")
	default:
		// TODO 挂载新设备，需要排除掉已经挂载过的旧设备，防止slave pod调度错误
		for _, oldSlavePod := range slavePods {
			devs := decodePodDevices(oldSlavePod.Annotations[AssignedIDsAnnotations])
			podDevices = append(podDevices, devs...)
		}
		usedUUIDs := getDevicesUUID(podDevices, container)
		if len(usedUUIDs) > 0 {
			annotations[GPUNoUseUUID] = strings.Join(usedUUIDs, ",")
		}
	}

	// TODO volcano vgpu 不考虑分多个pod申请资源
	pod := util.NewDeviceSlavePod(ownerPod, request, annotations)
	// TODO 让创建出来的slave pod只占用gpu，不包含设备文件
	env := v1.EnvVar{Name: NVIDIA_VISIBLE_DEVICES_ENV, Value: "none"}
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, env)
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

func (m *VolcanoVGPUMounter) MountDeviceInfoAfter(kubeClient *kubernetes.Clientset, cfg util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error {
	for _, slavePod := range slavePods {
		devMap := GetPodDevMap(slavePod)
		klog.Infoln("slave", slavePod.Name, "devices ", devMap)
		if config.AnnoIsExpansion(slavePod.Annotations) {
			// 扩容设备的操作
			// 默认调用一次命令，保证vgpu拦截库生成缓存
			_ = execNvidiaSMI(kubeClient, ownerPod, container)
			cacheFile := GetVGPUCacheFileDir(ownerPod, container)
			if err := MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
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
				klog.Infoln("Attach new vgpu devices to the vgpu container")
				// 默认调用一次命令，保证vgpu拦截库生成缓存
				_ = execNvidiaSMI(kubeClient, ownerPod, container)
				cacheFile := GetVGPUCacheFileDir(ownerPod, container)
				if err = MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
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
				klog.Infoln("Attach new vgpu devices to the ordinary container")
				// owner pod 无vgpu资源

				// 校验nvidia-smi命令行文件
				//if _, err := os.Stat(NVIDIA_SMI_FILE_PATH); err != nil {
				//	return fmt.Errorf("Failed to detect file [%s]: %v", NVIDIA_SMI_FILE_PATH, err)
				//}

				// 校验vgpu库文件
				if _, err := os.Stat(VGPU_LIBFILE_PATH); err != nil {
					return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_LIBFILE_PATH, err)
				}
				if _, err := os.Stat(VGPU_PRELOAD_PATH); err != nil {
					return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_PRELOAD_PATH, err)
				}
				// TODO 删除默认位置的vgpu缓存，防止 挂载->卸载->再挂载 失败
				_, _, _ = cfg.Execute("sh", "-c", "rm -f /tmp/cudevshr.cache")

				cmd := []string{"mkdir -p /etc /usr/bin /tmp/vgpu " + VGPU_DIR_PATH}
				_, _, err = client.ExecCmdToPod(kubeClient, ownerPod, container, cmd)
				if err != nil {
					// 这里先忽略失败
					klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
					cmd = []string{"sh", "-c", "mkdir -p /etc /usr/bin /tmp/vgpu " + VGPU_DIR_PATH}
					_, _, err = client.ExecCmdToPod(kubeClient, ownerPod, container, cmd)
				}
				if err != nil {
					klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
					cmd = []string{"bash", "-c", "mkdir -p /etc /usr/bin /tmp/vgpu " + VGPU_DIR_PATH}
					_, _, err = client.ExecCmdToPod(kubeClient, ownerPod, container, cmd)
				}
				if err != nil {
					klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
					return fmt.Errorf("Command [%s] call failed: %v", strings.Join(cmd, " "), err)
				}

				// 复制nvidia-smi命令行文件到目标容器
				//if _, _, err := client.CopyToPod(kubeClient, ownerPod, container,
				//	NVIDIA_SMI_FILE_PATH, "/usr/bin"); err != nil {
				//	// return fmt.Errorf("Copying file [%s] to container [%s] failed: %v", NVIDIA_SMI_FILE_PATH, NVIDIA_SMI_FILE_PATH, err)
				//	klog.Errorf("Copying file [%s] to container [%s] failed: %v", NVIDIA_SMI_FILE_PATH, NVIDIA_SMI_FILE_PATH, err)
				//}

				// 复制vgpu库到目标容器
				if _, _, err := client.CopyToPod(kubeClient, ownerPod, container,
					VGPU_LIBFILE_PATH, VGPU_LIBFILE_PATH); err != nil {
					return fmt.Errorf("Copying file [%s] to container [%s] failed: %v", VGPU_LIBFILE_PATH, VGPU_LIBFILE_PATH, err)
				}
				if _, _, err := client.CopyToPod(kubeClient, ownerPod, container,
					VGPU_PRELOAD_PATH, "/etc/ld.so.preload"); err != nil {
					return fmt.Errorf("Copying file [%s] to container [%s] failed: %v", VGPU_PRELOAD_PATH, "/etc/ld.so.preload", err)
				}

				shell := GetInitVGPUShell(GetVGPUEnvs(devMap))
				cmd = []string{"sh", "-c", "cat > /initVGPU.sh && chmod +x /initVGPU.sh && /initVGPU.sh"}
				_, _, err = client.WriteToPod(kubeClient, ownerPod, container, []byte(shell), cmd)
				if err != nil {
					return fmt.Errorf("Failed to initialize vgpu: %v", err)
				}
				nameStr := ownerPod.Annotations[InitVGPUAnnotations]
				var ctrNames []string
				if len(strings.TrimSpace(nameStr)) > 0 {
					ctrNames = strings.Split(strings.TrimSpace(nameStr), ",")
				}
				if !util.ContainsString(ctrNames, container.Name) {
					ctrNames = append(ctrNames, container.Name)
					annotations := map[string]string{InitVGPUAnnotations: strings.Join(ctrNames, ",")}
					if err = client.PatchPodAnnotations(kubeClient, ownerPod, annotations); err != nil {
						return fmt.Errorf("Failed to patch pod init vGPU: %v", err)
					}
				}

			}
		}
	}
	return nil
}

// 获取卸载的设备信息
func (m *VolcanoVGPUMounter) GetUnMountDeviceInfo(_ *kubernetes.Clientset, _ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
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

// 卸载设备成功前的后续动作
func (m *VolcanoVGPUMounter) UnMountDeviceInfoAfter(kubeClient *kubernetes.Clientset, cfg util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error {
	var tmpSlavePods []*v1.Pod
	for i, slavePod := range slavePods {
		if config.AnnoIsExpansion(slavePod.Annotations) {
			continue // 跳过用于扩容的pod
		}
		// 找到device-mounter挂载的设备pod
		tmpSlavePods = append(tmpSlavePods, slavePods[i])
	}
	cacheFile := GetVGPUCacheFileDir(ownerPod, container)
	if _, err := os.Stat(cacheFile); err == nil && len(tmpSlavePods) > 0 {
		// vgpu缓存存在，从缓存中剔除设备
		devMap := map[string]Device{}
		for _, slavePod := range tmpSlavePods {
			tmpMap := GetPodDevMap(slavePod)
			klog.Infoln("unmount slave", slavePod.Name, "devices ", devMap)
			for devuuid, dev := range tmpMap {
				devMap[devuuid] = dev
			}
		}
		_ = MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
			for i := int64(cache.num) - 1; i >= 0; i-- {
				devuuid := string(cache.uuids[i].uuid[:])[0:40]
				if _, ok := devMap[devuuid]; ok {
					cache.limit[i] = 0
					cache.sm_limit[i] = 0
					cache.uuids[i] = uuid{uuid: [96]byte{}}
					cache.num--
				}
			}
			return nil
		})
	}

	if nameStr, ok := ownerPod.Annotations[InitVGPUAnnotations]; ok {
		oldNames := strings.Split(nameStr, ",")
		newNames := util.DeleteSliceFunc(oldNames, func(s string) bool {
			return s != container.Name
		})

		if len(oldNames) != len(newNames) {
			annotations := map[string]string{InitVGPUAnnotations: strings.Join(newNames, ",")}
			if err := client.PatchPodAnnotations(kubeClient, ownerPod, annotations); err != nil {
				return fmt.Errorf("Failed to patch pod init vGPU: %v", err)
			}
			// 删除vgpu文件
			_, _, _ = cfg.Execute("sh", "-c", "rm -f /tmp/cudevshr.cache")
			_, _, _ = cfg.Execute("sh", "-c", "rm -f "+VGPU_LIBFILE_PATH)
			_, _, _ = cfg.Execute("sh", "-c", "rm -f /etc/ld.so.preload")
		}
	}
	return nil
}

func (m *VolcanoVGPUMounter) RecycledPodResources(_ *kubernetes.Clientset, _ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) []types.NamespacedName {
	slavePodKeys := make([]types.NamespacedName, 0)
	for _, slavePod := range slavePods {
		if config.AnnoIsExpansion(slavePod.Annotations) {
			continue // 跳过用于扩容的pod
		}
		slavePodKeys = append(slavePodKeys, types.NamespacedName{
			Name:      slavePod.Name,
			Namespace: slavePod.Namespace,
		})
	}
	return slavePodKeys
}
