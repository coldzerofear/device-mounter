package config

import (
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
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

type kubeletConfig struct {
	CgroupDriver string `yaml:"cgroupDriver"`
}

func InitializeCGroupDriver(cgroupDriver string) {
	initCGroupOnce.Do(func() {
		switch strings.ToLower(cgroupDriver) {
		case string(SYSTEMD):
			CurrentCGroupDriver = SYSTEMD
		case string(CGROUPFS):
			CurrentCGroupDriver = CGROUPFS
		default:
			configBytes, err := os.ReadFile(KubeletConfigPath)
			if err != nil {
				klog.Exitf("Read kubelet config file <%s> failed: %s", KubeletConfigPath, err.Error())
			}
			var kubelet kubeletConfig
			if err = yaml.Unmarshal(configBytes, &kubelet); err != nil {
				klog.Exitf("Failed to unmarshal kubelet config: %s", err.Error())
			}
			CurrentCGroupDriver = CGroupDriver(kubelet.CgroupDriver)
			if CurrentCGroupDriver != SYSTEMD && CurrentCGroupDriver != CGROUPFS {
				klog.Exitf("Invalid CGroup driver in kubelet config: %s", CurrentCGroupDriver)
			}
		}
	})
	klog.Infof("Current CGroup driver is %s", CurrentCGroupDriver)
}
