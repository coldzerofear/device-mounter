package util

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/coldzerofear/device-mounter/pkg/config"
	"github.com/opencontainers/runc/libcontainer/devices"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func IsSidecar(container v1.Container) bool {
	return container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways
}

func LoopRetry(retryCount uint, interval time.Duration, conditionFunc wait.ConditionFunc) error {
	var err error
	var done bool
	for i := uint(0); i < retryCount; i++ {
		done, err = conditionFunc()
		if done || err != nil {
			break
		}
		if i+1 == retryCount {
			err = fmt.Errorf("unable to complete after %d retries", retryCount)
			break
		}
		time.Sleep(interval)
	}
	return err
}

func CopyMap[K comparable, V any](src, dst map[K]V) {
	for k, v := range src {
		dst[k] = v
	}
}

func DeleteSliceFunc[S ~[]E, E any](s S, filter func(E) bool) S {
	if s == nil {
		return s
	}
	j := 0
	for _, e := range s {
		if filter(e) {
			s[j] = e
			j++
		}
	}
	return s[:j]
}

func CheckResourcesInNode(node *v1.Node, request map[v1.ResourceName]resource.Quantity) bool {
	if node == nil {
		return false
	}
	for reqRes, reqQuantity := range request {
		quantity, ok := node.Status.Allocatable[reqRes]
		if !ok {
			return false
		}
		if quantity.Value() < reqQuantity.Value() {
			return false
		}
	}
	return true
}

func CheckResourcesInSlice(resources map[v1.ResourceName]resource.Quantity, slice, ignore []string) bool {
	set := make(map[string]struct{})
	for _, s := range slice {
		set[s] = struct{}{}
		if quantity, ok := resources[v1.ResourceName(s)]; !ok {
			return false
		} else if quantity.IsZero() {
			return false
		}
	}
	for _, s := range ignore {
		set[s] = struct{}{}
	}
	for s, _ := range resources {
		if _, ok := set[string(s)]; !ok {
			return false
		}
	}
	return true
}

func NewDeviceSlavePod(ownerPod *v1.Pod, limits map[v1.ResourceName]resource.Quantity, annotations, labels map[string]string) *v1.Pod {
	namePrefix := fmt.Sprintf("%s-device-slave-", ownerPod.Name)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			//Name:      ownerPod.Name + "-slave-pod-" + randID,
			GenerateName: namePrefix,
			Annotations:  annotations,
			Labels:       labels,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "device-container",
					Image:           config.DeviceSlaveContainerImageTag,
					ImagePullPolicy: config.DeviceSlaveImagePullPolicy,
					Command:         []string{"/bin/sh", "-c"},
					//Args:            []string{"while true; do echo this is a device pool container; sleep 10;done"},
					Args: []string{"trap 'echo Ignoring termination signal; sleep infinity &' SIGTERM; while true; do echo this is a slave device container; sleep 10; done"},
					Resources: v1.ResourceRequirements{
						Limits: limits,
					},
				},
			},
			// PriorityClassName: "system-cluster-critical",
			SchedulerName:     ownerPod.Spec.SchedulerName,
			PriorityClassName: ownerPod.Spec.PriorityClassName,
			Priority:          ownerPod.Spec.Priority,
			RestartPolicy:     v1.RestartPolicyAlways,
			NodeSelector: map[string]string{
				v1.LabelHostname: ownerPod.Spec.NodeName,
			},
		},
		Status: v1.PodStatus{},
	}

}

func GetDeviceFileVersionV2(deviceFile string) (uint32, uint32, devices.Type, error) {
	deviceType := devices.BlockDevice
	info, err := os.Stat(deviceFile)
	if err != nil {
		return 0, 0, deviceType, fmt.Errorf("error getting file info: %s", err)
	}
	if (info.Mode() & os.ModeDevice) == 0 {
		return 0, 0, deviceType, fmt.Errorf("%s not a device file", deviceFile)
	}
	if (info.Mode() & os.ModeCharDevice) != 0 {
		deviceType = devices.CharDevice
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, deviceType, fmt.Errorf("error converting to syscall.Stat_t")
	}
	// 提取主设备号和次设备号
	major := unix.Major(stat.Rdev) // 主设备号
	minor := unix.Minor(stat.Rdev) // 次设备号
	return major, minor, deviceType, nil
}

func GetDeviceFileVersion(deviceFile string) (uint32, uint32, error) {
	info, err := os.Stat(deviceFile)
	if err != nil {
		return 0, 0, fmt.Errorf("Error getting file info: %s", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("error converting to syscall.Stat_t")
	}
	// 提取主设备号和次设备号
	major := unix.Major(stat.Rdev)
	minor := unix.Minor(stat.Rdev)
	return major, minor, nil
}
