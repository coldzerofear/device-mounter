package config

import (
	"strings"

	"k8s-device-mounter/pkg/api/v1alpha1"
)

const (
	DeviceTypeAnnotationKey = v1alpha1.Group + "/device-type"
	// 在原有基础上扩容，目前仅支持： volcano vgpu
	ExpansionAnnotationKey = v1alpha1.Group + "/expansion"
)

const (
	CreatedByLabelKey      = v1alpha1.Group + "/created-by"
	OwnerUidLabelKey       = v1alpha1.Group + "/owner-uid"
	MountContainerLabelKey = v1alpha1.Group + "/mounted-container"

	AppComponentLabelKey = "app.kubernetes.io/component"
	AppManagedByLabelKey = "app.kubernetes.io/managed-by"
	AppCreatedByLabelKey = "app.kubernetes.io/created-by"
	AppInstanceLabelKey  = "app.kubernetes.io/instance"
)

func AnnoIsExpansion(annos map[string]string) bool {
	if annos == nil {
		return false
	}
	return strings.ToLower(annos[ExpansionAnnotationKey]) == "true"
}
