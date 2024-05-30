package api

import "github.com/opencontainers/runc/libcontainer/devices"

type StatusCode uint32

const (
	// slave pod 成功
	Success StatusCode = 0
	// 等待 slave pod
	Wait StatusCode = 1
	// 跳过这个 slave pod, 跳过的pod最后阶段将被回收
	Skip StatusCode = 2
	// slave pod 不可调度
	Unschedulable StatusCode = 3
	// slave pod 失败
	Fail StatusCode = 4
)

type DeviceInfo struct {
	devices.Rule
	DeviceID       string
	DeviceFilePath string
}
