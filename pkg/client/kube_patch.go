package client

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type PatchMetadata struct {
	Annotations map[string]string `json:"annotations,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

func PatchPodAnnotations(ctx context.Context, kubeClient kubernetes.Interface, pod *corev1.Pod, annotations map[string]string) error {
	metadata := PatchMetadata{
		Annotations: annotations,
	}
	return PatchPodMetadata(ctx, kubeClient, pod, metadata)
}

func PatchPodMetadata(ctx context.Context, kubeClient kubernetes.Interface, pod *corev1.Pod, patchMetadata PatchMetadata) error {
	type patchPod struct {
		Metadata PatchMetadata `json:"metadata"`
	}
	p := patchPod{
		Metadata: patchMetadata,
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	rsPod, err := kubeClient.CoreV1().Pods(pod.Namespace).
		Patch(ctx, pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err == nil {
		rsPod.DeepCopyInto(pod)
	}
	return err
}

func PatchNodeMetadata(ctx context.Context, kubeClient kubernetes.Interface, nodeName string, patchMetadata PatchMetadata) error {
	type patchNode struct {
		Metadata PatchMetadata `json:"metadata"`
	}
	p := patchNode{
		Metadata: patchMetadata,
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kubeClient.CoreV1().Nodes().
		Patch(ctx, nodeName, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	return err
}
