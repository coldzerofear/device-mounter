package config

import (
	"os"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type CGroupDriver string

var (
	// device slave container image
	DeviceSlaveContainerImageTag = "alpine:latest"
	// device slave container image pull policy
	DeviceSlaveImagePullPolicy = v1.PullIfNotPresent

	CurrentCGroupDriver CGroupDriver
	initCGroupOnce      sync.Once
)

const (
	SYSTEMD  CGroupDriver = "systemd"
	CGROUPFS CGroupDriver = "cgroupfs"

	KubeletConfigPath = "/var/lib/kubelet/config.yaml"
)

func InitCGroupDriver() {
	initCGroupOnce.Do(func() {
		driver := os.Getenv("CGROUP_DRIVER")
		switch strings.ToLower(driver) {
		case string(SYSTEMD):
			CurrentCGroupDriver = SYSTEMD
		case string(CGROUPFS):
			CurrentCGroupDriver = CGROUPFS
		default:
			kubeletConfig, err := os.ReadFile(KubeletConfigPath)
			if err != nil {
				klog.Exitf("load kubelet config %s failed: %s", KubeletConfigPath, err.Error())
			}
			content := strings.ToLower(string(kubeletConfig))
			pos := strings.LastIndex(content, "cgroupdriver:")
			if pos < 0 {
				klog.Exitf("Unable to find cgroup driver in kubeletConfig file")
			}
			if strings.Contains(content, string(SYSTEMD)) {
				CurrentCGroupDriver = SYSTEMD
				return
			}
			if strings.Contains(content, string(CGROUPFS)) {
				CurrentCGroupDriver = CGROUPFS
				return
			}
			klog.Exitf("Unable to find cgroup driver in kubeletConfig file")
		}
	})
}
