package npu

import (
	"Ascend-device-plugin/pkg/common"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	DynamicResourceName = common.ResourceNamePrefix + common.AiCoreResourceName // 独占模式
)

func CheckRequestDynamicResources(request map[v1.ResourceName]resource.Quantity) bool {
	return util.CheckResourcesInSlice(request, []string{DynamicResourceName}, nil)
}
