package mounter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_PatchPod(t *testing.T) {
	mounterImpl := DeviceMounterServer{}
	tests := []struct {
		name    string
		pod     *v1.Pod
		patches []string
		want    *v1.Pod
	}{
		{
			name: "Explame 1",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "default",
				},
				Spec: v1.PodSpec{},
			},
			patches: []string{
				"{\"op\":\"replace\",\"path\":\"/metadata/name\",\"value\": \"newname\"}",
				"{\"op\":\"add\",\"path\":\"/spec/nodeName\",\"value\": \"node01\"}",
			},
			want: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "newname",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					NodeName: "node01",
				},
			},
		},
		{
			name: "Explame 2",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test2",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					NodeName: "node01",
				},
			},
			patches: []string{
				"{\"op\":\"replace\",\"path\":\"/metadata/namespace\",\"value\": \"kube-system\"}",
				"{\"op\":\"remove\",\"path\":\"/spec/nodeName\"}",
				"{\"op\": \"add\", \"path\": \"/spec/nodeName\", \"value\": \"node01\"}",
			},
			want: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test2",
					Namespace: "kube-system",
				},
				Spec: v1.PodSpec{
					NodeName: "node01",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod, err := mounterImpl.PatchPod(test.pod, test.patches)
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, test.want, pod)
		})
	}
}
