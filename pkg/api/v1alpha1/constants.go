package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	Group   = "device-mounter.io"
	Version = "v1alpha1"
)

var (
	GroupVersion = metav1.GroupVersionForDiscovery{
		GroupVersion: Group + "/" + Version,
		Version:      Version,
	}
	ApiGroup = metav1.APIGroup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIGroup",
			APIVersion: "v1",
		},
		Name:             Group,
		Versions:         []metav1.GroupVersionForDiscovery{GroupVersion},
		PreferredVersion: GroupVersion,
	}
	ApiGroupList = metav1.APIGroupList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIGroupList",
			APIVersion: "v1",
		},
		Groups: []metav1.APIGroup{ApiGroup},
	}
)
