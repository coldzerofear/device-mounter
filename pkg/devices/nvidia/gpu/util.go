package gpu

const (
	ResourceName = "nvidia.com/gpu"

	DEFAULT_NVIDIA_MAJOR_NUMBER    = 195
	DEFAULT_NVIDIACTL_MINOR_NUMBER = 255

	DEFAULT_CGROUP_PERMISSION = "rw"

	DEFAULT_DEVICE_FILE_PERMISSION = "666"

	NVIDIA_DEVICE_FILE_PREFIX         = "/dev/nvidia"
	NVIDIA_NVIDIACTL_FILE_PATH        = "/dev/nvidiactl"
	NVIDIA_NVIDIA_UVM_FILE_PATH       = "/dev/nvidia-uvm"
	NVIDIA_NVIDIA_UVM_TOOLS_FILE_PATH = "/dev/nvidia-uvm-tools"
)
