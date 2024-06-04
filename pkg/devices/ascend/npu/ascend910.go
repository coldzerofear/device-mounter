package npu

import (
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	ResourceNameAscend910     = ResourceNamePrefix + "/Ascend910"     // 独占模式
	ResourceNameAscend910_2c  = ResourceNamePrefix + "/Ascend910-2c"  // vnpu 2 core 1 gb
	ResourceNameAscend910_4c  = ResourceNamePrefix + "/Ascend910-4c"  // vnpu 4 core 2 gb
	ResourceNameAscend910_8c  = ResourceNamePrefix + "/Ascend910-8c"  // vnpu 8 core 4 gb
	ResourceNameAscend910_16c = ResourceNamePrefix + "/Ascend910-16c" // vnpu 16 core 8 gb
)

func CheckRequest910Resources(request map[v1.ResourceName]resource.Quantity) bool {
	resourceList := []string{ResourceNameAscend910, ResourceNameAscend910_2c,
		ResourceNameAscend910_4c, ResourceNameAscend910_8c, ResourceNameAscend910_16c}
	for _, resource := range resourceList {
		if util.CheckResourcesInSlice(request, []string{resource}, nil) {
			return true
		}
	}
	return false
}
