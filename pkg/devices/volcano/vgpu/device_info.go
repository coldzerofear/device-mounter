package vgpu

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/client"
	"github.com/coldzerofear/device-mounter/pkg/config"
	"github.com/coldzerofear/device-mounter/pkg/framework"
	"github.com/coldzerofear/device-mounter/pkg/util"
	"github.com/opencontainers/runc/libcontainer/devices"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
func (m *VolcanoVGPUMounter) GetDeviceType() string {
	return PluginName
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
func (m *VolcanoVGPUMounter) ValidateMountRequest(_ context.Context,
	_ *kubernetes.Clientset, node *v1.Node, ownerPod *v1.Pod, container *api.Container,
	request map[v1.ResourceName]resource.Quantity, annotations, _ map[string]string) error {

	if !util.CheckResourcesInSlice(request, []string{VolcanoVGPUNumber},
		[]string{VolcanoVGPUMemory, VolcanoVGPUCores, VolcanoVGPUMemoryPercentage}) {
		return api.NewMounterError(api.ResultCode_Fail, "Request for resources error")
	}
	if !util.CheckResourcesInNode(node, map[v1.ResourceName]resource.Quantity{
		VolcanoVGPUNumber: request[VolcanoVGPUNumber],
	}) {
		return api.NewMounterError(api.ResultCode_Insufficient, "Insufficient node resources")
	}

	expansion := config.AnnoIsExpansion(annotations)
	devs, hasVGPU := ownerPod.Annotations[AssignedIDsAnnotations]
	switch {
	case hasVGPU && expansion:
		podDevices := decodePodDevices(devs)
		usedUUIDs := getDevicesUUID(podDevices, container)
		quantity := request[VolcanoVGPUNumber]
		if quantity.Value() > int64(len(usedUUIDs)) {
			msg := "The requested resource count exceeds the target container resource count and cannot be expanded"
			return api.NewMounterError(api.ResultCode_Fail, msg)
		}
	case expansion:
		if _, err := os.Stat(GetVGPUCacheFileDir(ownerPod, container)); err != nil {
			msg := "The target container does not have vGPU resources and cannot be expanded"
			return api.NewMounterError(api.ResultCode_Fail, msg)
		}
	default:
		// 非扩展请求校验容器是否初始化过vGPU设备
		names := ownerPod.Annotations[InitVGPUAnnotations]
		ctrNames := strings.Split(strings.TrimSpace(names), ",")
		if slices.Contains(ctrNames, container.Name) {
			msg := "The target container has initialized the vGPU and cannot be mounted again"
			return api.NewMounterError(api.ResultCode_Fail, msg)
		}
	}

	return nil
}

// 构建要创建的奴隶pod模板
func (m *VolcanoVGPUMounter) BuildSupportPodTemplates(_ context.Context,
	ownerPod *v1.Pod, container *api.Container, request map[v1.ResourceName]resource.Quantity,
	annotations, labels map[string]string, slavePods []*v1.Pod) ([]*v1.Pod, error) {

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
	slavePod := util.NewDeviceSlavePod(ownerPod, request, annotations, labels)
	// TODO 让创建出来的slave pod只占用gpu，不包含设备文件
	env := v1.EnvVar{Name: NVIDIA_VISIBLE_DEVICES_ENV, Value: "none"}
	slavePod.Spec.Containers[0].Env = append(slavePod.Spec.Containers[0].Env, env)
	slavePod.Spec.SchedulerName = "volcano"
	slavePod.Spec.PriorityClassName = ownerPod.Spec.PriorityClassName
	return []*v1.Pod{slavePod}, nil
}

// 校验从属pod状态是否成功
func (m *VolcanoVGPUMounter) VerifySupportPodStatus(_ context.Context, slavePod *v1.Pod) (api.StatusCode, error) {
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
	if slavePod.Status.Conditions[0].Reason == v1.PodReasonUnschedulable ||
		slavePod.Status.Conditions[0].Reason == v1.PodReasonSchedulerError {
		err := api.NewMounterError(api.ResultCode_Insufficient, slavePod.Status.Conditions[0].Message)
		return api.Unschedulable, err
	}
	return api.Wait, nil
}

// 获取待挂载的设备信息
func (m *VolcanoVGPUMounter) GetDeviceInfosToMount(_ context.Context, _ *kubernetes.Clientset,
	ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {

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

func addVGPUResource(cacheFile string, devMap map[string]Device) error {
	return MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
		for i := uint64(0); i < cache.num; i++ {
			devuuid := string(cache.uuids[i].uuid[:])[0:40]
			if dev, ok := devMap[devuuid]; ok {
				cores := uint64(dev.Usedcores)
				memory := uint64(dev.Usedmem) << 20 // mb转bytes
				klog.Infoln("add device resource", devuuid, "add memory", memory, "add core", cores)
				cache.limit[i] += memory
				cache.smLimit[i] += cores
			}
		}
		return nil
	})
}

func subVGPUResource(cacheFile string, devMap map[string]Device) error {
	return MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
		for i := uint64(0); i < cache.num; i++ {
			devuuid := string(cache.uuids[i].uuid[:])[0:40]
			if dev, ok := devMap[devuuid]; ok {
				cores := uint64(dev.Usedcores)
				memory := uint64(dev.Usedmem) << 20 // mb转bytes
				klog.Infoln("sub device resource", devuuid, "sub memory", memory, "sub core", cores)
				cache.limit[i] -= memory
				cache.smLimit[i] -= cores
			}
		}
		return nil
	})
}

func ExpansionVGPUDevice(ctx context.Context, kubeClient *kubernetes.Clientset,
	ownerPod *v1.Pod, container *api.Container, devMap map[string]Device) (RollBackFunc, error) {
	// 默认调用一次命令，保证vgpu拦截库生成缓存
	_ = execNvidiaSMI(ctx, kubeClient, ownerPod, container)
	cacheFile := GetVGPUCacheFileDir(ownerPod, container)
	if err := addVGPUResource(cacheFile, devMap); err != nil {
		return util.NilCloser, err
	}
	rollBackFunc := func() error {
		return subVGPUResource(cacheFile, devMap)
	}
	return rollBackFunc, nil
}

func (m *VolcanoVGPUMounter) ExecutePostMountActions(ctx context.Context, kubeClient *kubernetes.Clientset,
	cfg util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error {
	for _, slavePod := range slavePods {
		devMap := GetPodDevMap(slavePod)
		klog.Infoln("slave", slavePod.Name, "devices ", devMap)
		// 检测是否存在vgpu缓存
		_, fileErr := os.Stat(GetVGPUCacheFileDir(ownerPod, container))
		switch {
		case config.AnnoIsExpansion(slavePod.Annotations): // 扩容设备操作
			klog.Infoln("Expansion vGPU devices to the vGPU container")
			_, err := ExpansionVGPUDevice(ctx, kubeClient, ownerPod, container, devMap)
			if err != nil {
				return err
			}
		case HasVGPU(ownerPod, container) || fileErr == nil: // ownerPod 存在vGPU资源
			klog.Infoln("Attach new vGPU devices to the vGPU container")
			// 默认调用一次命令，保证vgpu拦截库生成缓存
			_ = execNvidiaSMI(ctx, kubeClient, ownerPod, container)
			cacheFile := GetVGPUCacheFileDir(ownerPod, container)
			if err := MutationCacheFunc(cacheFile, func(cache *sharedRegionT) error {
				for i := uint64(0); i < cache.num; i++ {
					devuuid := string(cache.uuids[i].uuid[:])[0:40]
					if dev, ok := devMap[devuuid]; ok {
						cores := uint64(dev.Usedcores)
						memory := uint64(dev.Usedmem) << 20 // mb转bytes
						//klog.Infoln("Expansion device", devuuid, "add memory", memory, "add core", cores)
						klog.Infoln("Attach new device", devuuid, "memory limit", memory, "core limit", cores)
						cache.limit[i] = memory
						cache.smLimit[i] = cores
						delete(devMap, devuuid)
					}
				}
				for devuuid, dev := range devMap {
					tail := cache.num
					cores := uint64(dev.Usedcores)
					memory := uint64(dev.Usedmem) << 20 // mb转bytes
					klog.Infoln("Attach new device", devuuid, "memory limit", memory, "core limit", cores)
					cache.uuids[tail] = ConvertUUID(devuuid)
					cache.limit[tail] = memory
					cache.smLimit[tail] = cores
					cache.num++
				}
				return nil
			}); err != nil {
				return err
			}
		default: // ownerPod 没有vGPU资源
			klog.Infoln("Attach new vGPU devices to the ordinary container")
			// 校验宿主机上的vGPU库文件，确保正确安装了volcano-vgpu-device-plugin
			if _, err := os.Stat(VGPU_LIBFILE_PATH); err != nil {
				return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_LIBFILE_PATH, err)
			}
			if _, err := os.Stat(VGPU_PRELOAD_PATH); err != nil {
				return fmt.Errorf("Failed to detect file [%s]: %v", VGPU_PRELOAD_PATH, err)
			}

			// TODO 删除默认位置的vgpu缓存，防止 挂载->卸载->再挂载 失败
			_, _, _ = cfg.Execute("sh", "-c", "rm -f /tmp/cudevshr.cache")
			_, _, _ = cfg.Execute("sh", "-c", "rm -rf /tmp/vgpu")
			_, _, _ = cfg.Execute("sh", "-c", "mkdir -p /tmp/vgpu")
			_, _, _ = cfg.Execute("sh", "-c", "mkdir -p "+VGPU_DIR_PATH)

			// 复制vgpu库到目标容器
			if _, _, err := client.CopyToPod(kubeClient, ownerPod, container, VGPU_LIBFILE_PATH, VGPU_LIBFILE_PATH); err != nil {
				return fmt.Errorf("Copying file [%s] to container [%s] failed: %v", VGPU_LIBFILE_PATH, VGPU_LIBFILE_PATH, err)
			}
			if _, _, err := client.CopyToPod(kubeClient, ownerPod, container, VGPU_PRELOAD_PATH, "/etc/ld.so.preload"); err != nil {
				_, _, _ = cfg.Execute("sh", "-c", "rm -f "+VGPU_LIBFILE_PATH)
				return fmt.Errorf("Copying file [%s] to container [%s] failed: %v", VGPU_PRELOAD_PATH, "/etc/ld.so.preload", err)
			}

			shell := GetInitVGPUShell(GetVGPUEnvs(devMap))
			cmd := []string{"sh", "-c", "cat > /initVGPU.sh && chmod +x /initVGPU.sh && /initVGPU.sh"}
			if _, _, err := client.WriteToPod(ctx, kubeClient, ownerPod, container, []byte(shell), cmd); err != nil {
				_, _, _ = cfg.Execute("sh", "-c", "rm -f "+VGPU_LIBFILE_PATH)
				_, _, _ = cfg.Execute("sh", "-c", "rm -f /etc/ld.so.preload")
				return fmt.Errorf("Failed to initialize vGPU: %v", err)
			}
			var contNames []string
			names := ownerPod.Annotations[InitVGPUAnnotations]
			if names = strings.TrimSpace(names); len(names) > 0 {
				contNames = strings.Split(names, ",")
			}
			if !slices.Contains(contNames, container.Name) {
				contNames = append(contNames, container.Name)
				annotations := map[string]string{InitVGPUAnnotations: strings.Join(contNames, ",")}
				if err := client.PatchPodAnnotations(ctx, kubeClient, ownerPod, annotations); err != nil {
					_, _, _ = cfg.Execute("sh", "-c", "rm -f /initVGPU.sh")
					_, _, _ = cfg.Execute("sh", "-c", "rm -f /tmp/cudevshr.cache")
					_, _, _ = cfg.Execute("sh", "-c", "rm -rf /tmp/vgpu")
					_, _, _ = cfg.Execute("sh", "-c", "rm -f "+VGPU_LIBFILE_PATH)
					_, _, _ = cfg.Execute("sh", "-c", "rm -f /etc/ld.so.preload")
					return fmt.Errorf("Failed to patch pod annotation [%s]: %v", InitVGPUAnnotations, err)
				}
			}
		}
	}
	return nil
}

// 获取卸载的设备信息
func (m *VolcanoVGPUMounter) GetDeviceInfosToUnmount(_ context.Context, _ *kubernetes.Clientset,
	_ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) ([]api.DeviceInfo, error) {
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
func (m *VolcanoVGPUMounter) GetDevicesActiveProcessIDs(_ context.Context,
	containerPids []int, deviceInfos []api.DeviceInfo) ([]int, error) {
	var pids []int
	for _, info := range deviceInfos {
		if err := DeviceRunningProcessFunc(info.DeviceID, func(process nvml.ProcessInfo) {
			if slices.Contains(containerPids, int(process.Pid)) {
				pids = append(pids, int(process.Pid))
			}
		}); err != nil {
			return nil, err
		}
	}
	return pids, nil
}

// 卸载设备成功前的后续动作
func (m *VolcanoVGPUMounter) ExecutePostUnmountActions(ctx context.Context, kubeClient *kubernetes.Clientset,
	cfg util.Config, ownerPod *v1.Pod, container *api.Container, slavePods []*v1.Pod) error {

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
					cache.smLimit[i] = 0
					cache.uuids[i] = uuid{uuid: [96]byte{}}
					cache.num--
				}
			}
			return nil
		})
	}

	if names := strings.TrimSpace(ownerPod.Annotations[InitVGPUAnnotations]); len(names) > 0 {
		oldNames := strings.Split(names, ",")
		newNames := util.DeleteSliceFunc(oldNames, func(s string) bool {
			return s != container.Name
		})
		if len(oldNames) != len(newNames) {
			annotations := map[string]string{InitVGPUAnnotations: strings.Join(newNames, ",")}
			err := client.PatchPodAnnotations(ctx, kubeClient, ownerPod, annotations)
			if err != nil {
				return fmt.Errorf("Failed to patch pod annotation [%s]: %v", InitVGPUAnnotations, err)
			}
			// 删除vgpu文件
			_, _, _ = cfg.Execute("sh", "-c", "rm -f /tmp/cudevshr.cache")
			_, _, _ = cfg.Execute("sh", "-c", "rm -f "+VGPU_LIBFILE_PATH)
			_, _, _ = cfg.Execute("sh", "-c", "rm -f /etc/ld.so.preload")
		}
	}

	return nil
}

func (m *VolcanoVGPUMounter) GetPodsToCleanup(_ context.Context, _ *kubernetes.Clientset,
	_ *v1.Pod, _ *api.Container, slavePods []*v1.Pod) []api.ObjectKey {
	podKeys := make([]api.ObjectKey, 0, len(slavePods))
	for _, slavePod := range slavePods {
		if config.AnnoIsExpansion(slavePod.Annotations) {
			continue // 跳过用于扩容的pod
		}
		podKeys = append(podKeys, api.ObjectKeyFromObject(slavePod))
	}
	return podKeys
}
