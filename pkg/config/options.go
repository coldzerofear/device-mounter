package config

import v1 "k8s.io/api/core/v1"

var (
	DeviceSlaveContainerImageTag = "alpine:latest"

	DeviceSlaveImagePullPolicy = v1.PullIfNotPresent

	//	DeviceSlavePodNamespace = "device-mount-pool"

	CGroupDriver = ""
)
