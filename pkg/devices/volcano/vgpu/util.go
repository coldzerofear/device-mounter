package vgpu

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/api/v1alpha1"
	"github.com/coldzerofear/device-mounter/pkg/client"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	uuid2 "k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/keymutex"
)

const (
	PluginName = "VOLCANO_VGPU"

	InitVGPUAnnotations = v1alpha1.Group + "/initVGPU"

	AssignedIDsAnnotations = "volcano.sh/vgpu-ids-new"

	GPUInUse = "nvidia.com/use-gputype"
	GPUNoUse = "nvidia.com/nouse-gputype"
	// GPUUseUUID is user can use specify GPU device for set GPU UUID.
	GPUUseUUID = "nvidia.com/use-gpuuuid"
	// GPUNoUseUUID is user can not use specify GPU device for set GPU UUID.
	GPUNoUseUUID = "nvidia.com/nouse-gpuuuid"

	VolcanoVGPUNumber           = "volcano.sh/vgpu-number"
	VolcanoVGPUMemory           = "volcano.sh/vgpu-memory"
	VolcanoVGPUCores            = "volcano.sh/vgpu-cores"
	VolcanoVGPUMemoryPercentage = "volcano.sh/vgpu-memory-percentage"

	DEFAULT_NVIDIA_MAJOR_NUMBER    = 195
	DEFAULT_NVIDIACTL_MINOR_NUMBER = 255

	DEFAULT_CGROUP_PERMISSION = "rw"

	NVIDIA_DEVICE_FILE_PREFIX         = "/dev/nvidia"
	NVIDIA_NVIDIACTL_FILE_PATH        = "/dev/nvidiactl"
	NVIDIA_NVIDIA_UVM_FILE_PATH       = "/dev/nvidia-uvm"
	NVIDIA_NVIDIA_UVM_TOOLS_FILE_PATH = "/dev/nvidia-uvm-tools"
	NVIDIA_SMI_FILE_PATH              = "/usr/bin/nvidia-smi"

	DRIVER_VERSION_PROC_PATH = "/proc/driver/nvidia/version"

	VGPU_DIR_PATH     = "/usr/local/vgpu"
	VGPU_LIBFILE_PATH = VGPU_DIR_PATH + "/libvgpu.so"
	VGPU_PRELOAD_PATH = VGPU_DIR_PATH + "/ld.so.preload"

	NVIDIA_VISIBLE_DEVICES_ENV          = "NVIDIA_VISIBLE_DEVICES"
	CUDA_DEVICE_SM_LIMIT_ENV            = "CUDA_DEVICE_SM_LIMIT"
	CUDA_DEVICE_MEMORY_LIMIT_ENV        = "CUDA_DEVICE_MEMORY_LIMIT"
	CUDA_DEVICE_MEMORY_SHARED_CACHE_ENV = "CUDA_DEVICE_MEMORY_SHARED_CACHE"
	// 虚拟显存超卖 TRUE/FALSE
	CUDA_OVERSUBSCRIBE_ENV = "CUDA_OVERSUBSCRIBE"
	// GPU算力拦截开关 FORCE/DISABLE
	GPU_CORE_UTILIZATION_POLICY_ENV = "GPU_CORE_UTILIZATION_POLICY"
)

type deviceMemory struct {
	contextSize uint64
	moduleSize  uint64
	bufferSize  uint64
	offset      uint64
	total       uint64
	unused      [3]uint64
}

type deviceUtilization struct {
	decUtil uint64
	encUtil uint64
	smUtil  uint64
	unused  [3]uint64
}

type shrregProcSlotT struct {
	pid         int32
	hostpid     int32
	used        [16]deviceMemory
	monitorused [16]uint64
	deviceUtil  [16]deviceUtilization
	status      int32
	unused      [3]uint64
}

type uuid struct {
	uuid [96]byte
}

type semT struct {
	sem [32]byte
}

type sharedRegionT struct {
	initializedFlag int32
	majorVersion    int32
	minorVersion    int32
	smInitFlag      int32
	ownerPid        uint32
	sem             semT
	num             uint64
	uuids           [16]uuid

	limit   [16]uint64
	smLimit [16]uint64
	procs   [1024]shrregProcSlotT

	procnum           int32
	utilizationSwitch int32
	recentKernel      int32
	priority          int32
	lastKernelTime    int64
	unused            [4]uint64
}

type RollBackFunc func() error

func mmapVGPUCacheConfig(cacheDir string) (*sharedRegionT, []byte, error) {
	files, err := ioutil.ReadDir(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	// 空目录
	if len(files) == 0 {
		return nil, nil, errors.New("cannot find vgpu cache file")
	}
	for _, file := range files {
		// 跳过hook的动态链接库
		//if strings.Contains(file.Name(), "libvgpu.so") {
		//	continue
		//}
		// 跳过非cache文件
		if !strings.Contains(file.Name(), ".cache") {
			continue
		}
		cacheFilePath := fmt.Sprintf("%s/%s", cacheDir, file.Name())
		cache, data, err := mmapVGPUCacheFile(cacheFilePath)
		if err != nil {
			klog.Errorln(err)
		} else {
			return cache, data, nil
		}
	}
	return nil, nil, errors.New("cannot find vgpu cache file")
}

func mmapVGPUCacheFile(filePath string) (*sharedRegionT, []byte, error) {
	f, err := os.OpenFile(filePath, os.O_RDWR, 0666)
	if err != nil {
		return nil, nil, fmt.Errorf("open file %s err: %v", filePath, err)
	}
	defer f.Close()

	ss, _ := f.Stat()
	var size = unsafe.Sizeof(sharedRegionT{})
	if ss.Size() < int64(size) {
		return nil, nil, fmt.Errorf("cache file %s size %d is less than %d", filePath, ss.Size(), size)
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("mmap file %s err: %v", filePath, err)
	}
	// 安全地将映射的内存转换为sharedRegionT的指针
	cachestr := (*sharedRegionT)(unsafe.Pointer(&data[0]))
	klog.V(4).Infoln("mmap file sizeof:", size, "cachestr.num:", cachestr.num, "cachestr.utilizationSwitch:", cachestr.utilizationSwitch, "cachestr.recentKernel:", cachestr.recentKernel)
	return cachestr, data, nil
}

type Device struct {
	CtrIdx    uint32
	UUID      string
	Type      string
	Usedmem   int32 // 单位mb
	Usedcores int32
}

type ContainerDevices []Device

func getDevicesUUID(podDevices []ContainerDevices, container *api.Container) []string {
	usedUUID := sets.NewString()
	for _, devices := range podDevices {
		for _, device := range devices {
			if device.CtrIdx == container.Index {
				usedUUID.Insert(device.UUID)
			}
		}
	}
	return usedUUID.List()
}

func decodePodDevices(str string) []ContainerDevices {
	if len(str) == 0 {
		return []ContainerDevices{}
	}
	var pd []ContainerDevices
	for _, s := range strings.Split(str, ";") {
		cd := decodeContainerDevices(s)
		pd = append(pd, cd)
	}
	return pd
}

func decodeContainerDevices(str string) ContainerDevices {
	if len(str) == 0 {
		return ContainerDevices{}
	}
	cd := strings.Split(str, ":")
	contdev := ContainerDevices{}
	tmpdev := Device{}
	for _, val := range cd {
		if strings.Contains(val, ",") {
			tmpstr := strings.Split(val, ",")
			idx, _ := strconv.ParseInt(tmpstr[0], 10, 32)
			tmpdev.CtrIdx = uint32(idx)
			tmpdev.UUID = tmpstr[1]
			tmpdev.Type = tmpstr[2]
			devmem, _ := strconv.ParseInt(tmpstr[3], 10, 32)
			tmpdev.Usedmem = int32(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[4], 10, 32)
			tmpdev.Usedcores = int32(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	return contdev
}

func GetDeviceMinorByUUID(uuid string) (int, error) {
	if rt := nvml.Init(); rt != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml Init error: %s", nvml.ErrorString(rt))
	}
	defer nvml.Shutdown()

	dev, rt := nvml.DeviceGetHandleByUUID(uuid)
	if rt != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml DeviceGetHandleByUUID error: %s", nvml.ErrorString(rt))
	}
	minor, rt := dev.GetMinorNumber()
	if rt != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml DeviceGetMinorNumber error: %s", nvml.ErrorString(rt))
	}
	return minor, nil
}

func DeviceRunningProcessFunc(uuid string, f func(process nvml.ProcessInfo)) error {
	if rt := nvml.Init(); rt != nvml.SUCCESS {
		return fmt.Errorf("nvml Init error: %s", nvml.ErrorString(rt))
	}
	defer nvml.Shutdown()

	dev, rt := nvml.DeviceGetHandleByUUID(uuid)
	if rt != nvml.SUCCESS {
		return fmt.Errorf("nvml DeviceGetHandleByUUID error: %s", nvml.ErrorString(rt))
	}
	processes, rt := dev.GetComputeRunningProcesses()
	if rt != nvml.SUCCESS {
		return fmt.Errorf("nvml DeviceGetComputeRunningProcesses error: %s", nvml.ErrorString(rt))
	}
	for _, process := range processes {
		if f != nil {
			f(process)
		}
	}
	processes, rt = dev.GetGraphicsRunningProcesses()
	if rt != nvml.SUCCESS {
		return fmt.Errorf("nvml DeviceGetGraphicsRunningProcesses error: %s", nvml.ErrorString(rt))
	}
	for _, process := range processes {
		if f != nil {
			f(process)
		}
	}
	return nil
}

func HasVGPU(pod *v1.Pod, container *api.Container) bool {
	c := pod.Spec.Containers[container.Index]
	q, ok := c.Resources.Limits[VolcanoVGPUNumber]
	return ok && !q.IsZero()
}

func GetVGPUCacheFileDir(pod *v1.Pod, container *api.Container) string {
	return fmt.Sprintf("/tmp/vgpu/containers/%s_%s", string(pod.UID), container.Name)
}

func GetPodDevMap(pod *v1.Pod) map[string]Device {
	devMap := map[string]Device{}
	podDevices := decodePodDevices(pod.Annotations[AssignedIDsAnnotations])
	for _, devs := range podDevices {
		for _, dev := range devs {
			devMap[dev.UUID] = dev
		}
	}
	return devMap
}

var (
	cacheLock keymutex.KeyMutex
)

func init() {
	n := runtime.NumCPU() * 2
	if n < 4 {
		n = 4
	}
	cacheLock = keymutex.NewHashed(n)
}

func MutationCacheFunc(cacheDir string, mutationFunc func(*sharedRegionT) error) error {
	// 修改配置文件限制值
	cacheConfig, data, err := mmapVGPUCacheConfig(cacheDir)
	if err != nil {
		return err
	}
	cacheLock.LockKey(cacheDir)
	defer func() {
		_ = cacheLock.UnlockKey(cacheDir)
		if data != nil {
			err = syscall.Munmap(data)
		}
		if err != nil {
			klog.Errorf("Munmap cache file failed[%s]: %v", cacheDir, err)
		}
	}()
	return mutationFunc(cacheConfig)
}

func ConvertUUID(devuuid string) uuid {
	uuid := uuid{uuid: [96]byte{}}
	for i, b := range devuuid {
		uuid.uuid[i] = byte(b)
	}
	uuid.uuid[len(devuuid)] = byte(0) // c语言字符串\0结尾
	return uuid
}

func getSharedCache() string {
	return fmt.Sprintf("/tmp/vgpu/%v.cache", uuid2.NewUUID())
}

func GetVGPUEnvs(devMap map[string]Device) []string {
	var (
		uuids        []string
		smLimitEnvs  []string
		memLimitEnvs []string
		smPolicy     = "DISABLE"
		index        = 0
	)
	for devuuid, dev := range devMap {
		uuids = append(uuids, devuuid)
		limitKey := fmt.Sprintf("%s_%d", CUDA_DEVICE_MEMORY_LIMIT_ENV, index)
		memLimitEnvs = append(memLimitEnvs, fmt.Sprintf("%s=%vm", limitKey, dev.Usedmem))
		if dev.Usedcores > 0 && dev.Usedcores < 100 {
			smPolicy = "FORCE"
		}
		limitKey = fmt.Sprintf("%s_%d", CUDA_DEVICE_SM_LIMIT_ENV, index)
		smLimitEnvs = append(smLimitEnvs, fmt.Sprintf("%s=%v", limitKey, dev.Usedcores))
		index++
	}
	smPolicyEnv := fmt.Sprintf("%s=%s", GPU_CORE_UTILIZATION_POLICY_ENV, smPolicy)
	// sharedCacheEnv := fmt.Sprintf("%s=%s", CUDA_DEVICE_MEMORY_SHARED_CACHE_ENV, getSharedCache())
	devicesEnv := fmt.Sprintf("%s=%s", NVIDIA_VISIBLE_DEVICES_ENV, strings.Join(uuids, ","))
	envs := append(smLimitEnvs, memLimitEnvs...)
	envs = append(envs, smPolicyEnv, devicesEnv)
	return envs
}

func GetInitVGPUShell(envs []string) string {
	initShell := "#!/bin/sh\n"
	for _, env := range envs {
		initShell += fmt.Sprintf("export %s\n", env)
	}
	initShell += NVIDIA_SMI_FILE_PATH
	return initShell
}

func execNvidiaSMI(ctx context.Context, kubeClient *kubernetes.Clientset, ownerPod *v1.Pod, container *api.Container) error {
	cmd := []string{"nvidia-smi"}
	_, _, err := client.ExecCmdToPod(ctx, kubeClient, ownerPod, container, cmd)
	if err != nil {
		// 这里先忽略失败
		klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
		cmd = []string{"sh", "-c", "nvidia-smi"}
		_, _, err = client.ExecCmdToPod(ctx, kubeClient, ownerPod, container, cmd)
	}
	if err != nil {
		klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
		cmd = []string{"bash", "-c", "nvidia-smi"}
		_, _, err = client.ExecCmdToPod(ctx, kubeClient, ownerPod, container, cmd)
	}
	if err != nil {
		klog.Errorf("try exec [%s] cmd failed: %v", strings.Join(cmd, " "), err)
	}
	return err
}
