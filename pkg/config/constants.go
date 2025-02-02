package config

import (
	"strings"

	"github.com/coldzerofear/device-mounter/pkg/api/v1alpha1"
)

const (
	DeviceTypeAnnotationKey  = v1alpha1.Group + "/device-type"
	ContainerIdAnnotationKey = v1alpha1.Group + "/containerId"

	// 在原有基础上扩容，目前仅支持： volcano vgpu
	ExpansionAnnotationKey = v1alpha1.Group + "/expansion"
	// 快速分配并占用设备
	FastAllocateAnnotationKey = v1alpha1.Group + "/fast-allocate"
)

const (
	CreatedByLabelKey      = v1alpha1.Group + "/created-by"
	OwnerNameLabelKey      = v1alpha1.Group + "/owner-name"
	OwnerUidLabelKey       = v1alpha1.Group + "/owner-uid"
	MountContainerLabelKey = v1alpha1.Group + "/mounted-container"

	AppComponentLabelKey = "app.kubernetes.io/component"
	AppManagedByLabelKey = "app.kubernetes.io/managed-by"
	AppCreatedByLabelKey = "app.kubernetes.io/created-by"
	AppInstanceLabelKey  = "app.kubernetes.io/instance"

	CreateManagerBy = "device-mounter"
)

func AnnoIsExpansion(annos map[string]string) bool {
	return annos != nil && strings.EqualFold(strings.TrimSpace(annos[ExpansionAnnotationKey]), "true")
}
