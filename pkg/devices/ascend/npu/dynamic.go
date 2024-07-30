package npu

import (
	"Ascend-device-plugin/pkg/common"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	DynamicResourceName = common.ResourceNamePrefix + common.AiCoreResourceName // 动态资源
)

func CheckRequestDynamicResources(request map[v1.ResourceName]resource.Quantity, annotations map[string]string) bool {
	if !util.CheckResourcesInSlice(request, []string{DynamicResourceName}, nil) {
		return false
	}
	if annotations == nil || len(annotations[DynamicResourceName]) == 0 {
		return false
	}
	return true
}
