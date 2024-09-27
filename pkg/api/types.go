package api

import (
	"github.com/opencontainers/runc/libcontainer/devices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

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

type ObjectKey struct {
	types.NamespacedName
	UID *string
}

func ObjectKeyFromObject(object metav1.Object) ObjectKey {
	key := ObjectKey{
		NamespacedName: types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		},
	}
	if object.GetUID() != "" {
		key.UID = pointer.String(string(object.GetUID()))
	}
	return key
}

func NewMounterError(code ResultCode, msg string) *MounterError {
	return &MounterError{
		Code:    code,
		Message: msg,
	}
}

type MounterError struct {
	Code    ResultCode
	Message string
}

func (e *MounterError) Error() string {
	return e.Message
}
