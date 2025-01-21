package client

import (
	"context"
	"testing"

	"github.com/coldzerofear/device-mounter/pkg/api"
	v1 "k8s.io/api/core/v1"
)

func Test_CopyToPod(t *testing.T) {
	t.Skip("Skipping test because condition is met.")
	kubeconfigPath := "/root/.kube/config-37"
	kubeClient := GetKubeClient(kubeconfigPath)
	targetPod := &v1.Pod{}
	targetPod.Name = "device-mounter-daemonset-77xxt"
	targetPod.Namespace = "kube-system"
	container := &api.Container{Name: "mounter"}
	_, _, err := CopyToPod(kubeClient, targetPod, container, "/root/.kube/config-37", "/root/222/config-37")
	if err != nil {
		t.Error(err)
	}
	_, _, err = CopyToPod(kubeClient, targetPod, container, "/root/.kube/config-37", "/etc")
	if err != nil {
		t.Error(err)
	}
}

func Test_ExecCmdToPod(t *testing.T) {
	t.Skip("Skipping test because condition is met.")
	kubeconfigPath := "/root/.kube/config-37"
	kubeClient := GetKubeClient(kubeconfigPath)
	pod := &v1.Pod{}
	pod.Namespace = "default"
	pod.Name = "gpu-pod"
	container := &api.Container{Name: "ubuntu-container"}
	//cmd := []string{"sh", "-c", "mkdir -p /etc && mkdir -p /usr/bin && mkdir -p /usr/local/vgpu"}
	cmd := []string{"sh", "-c", "export CUDA_DEVICE_SM_LIMIT_0=0 CUDA_DEVICE_MEMORY_LIMIT_0=1000m GPU_CORE_UTILIZATION_POLICY=DISABLE CUDA_DEVICE_MEMORY_SHARED_CACHE=/tmp/vgpu/6255821a-40bb-4093-8cf4-8696503ea138.cache NVIDIA_VISIBLE_DEVICES=GPU-c496852d-f5df-316c-e2d5-86f0b322ec4c"}
	_, _, err := ExecCmdToPod(context.TODO(), kubeClient, pod, container, cmd)
	if err != nil {
		t.Error(err)
	}
}

func Test_WriteToPod(t *testing.T) {
	t.Skip("Skipping test because condition is met.")
	kubeconfigPath := "/root/.kube/config-37"
	kubeClient := GetKubeClient(kubeconfigPath)
	pod := &v1.Pod{}
	pod.Namespace = "default"
	pod.Name = "gpu-pod"
	container := &api.Container{Name: "ubuntu-container"}
	content := "#!/bin/sh\nexport CUDA_DEVICE_SM_LIMIT_0=0\nexport CUDA_DEVICE_MEMORY_LIMIT_0=1000m\nexport GPU_CORE_UTILIZATION_POLICY=DISABLE\nexport NVIDIA_VISIBLE_DEVICES=GPU-c496852d-f5df-316c-e2d5-86f0b322ec4c\nnvidia-smi"
	cmd := []string{"sh", "-c", "cat > /initVGPU.sh && chmod +x /initVGPU.sh && /initVGPU.sh"}
	_, _, err := WriteToPod(context.TODO(), kubeClient, pod, container, []byte(content), cmd)
	if err != nil {
		t.Error(err)
	}
}
