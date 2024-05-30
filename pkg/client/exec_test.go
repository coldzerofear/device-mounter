//go:build ignore
// +build ignore

package client

import (
	"fmt"
	"testing"

	"k8s-device-mounter/pkg/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubectl/pkg/cmd/cp"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/utils/pointer"
)

func Test_CopyToPod(t *testing.T) {
	t.Skip("Skipping test because condition is met.")

	kubeconfigPath := "/root/.kube/config-37"
	kubeClient := GetKubeClient(kubeconfigPath)
	kubeConfig := GetKubeConfig(kubeconfigPath)
	kubeConfig.APIPath = "/api"
	kubeConfig.GroupVersion = &schema.GroupVersion{Version: "v1"} // this targets the core api groups so the url path will be /api/v1
	kubeConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}

	cf := genericclioptions.NewConfigFlags(true)
	cf.Namespace = pointer.String("default")

	f := util.NewFactory(cf)
	ioStreams, _, stdout, stderr := genericiooptions.NewTestIOStreams()
	cmd := cp.NewCmdCp(f, ioStreams)
	opts := cp.NewCopyOptions(ioStreams)
	err := opts.Complete(f, cmd, []string{"/root/.kube/config-37", fmt.Sprintf("default/gpu-pod:%s", "/root/")})
	if err != nil {
		fmt.Println(err)
		return
	}
	opts.ClientConfig = kubeConfig
	opts.Clientset = kubeClient
	opts.Namespace = "default"
	opts.Container = "ubuntu-container"
	if err = opts.Run(); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("STDOUT:", stdout.String())
	fmt.Println("STDERR:", stderr.String())
}

func Test_ExecCmd(t *testing.T) {
	t.Skip("Skipping test because condition is met.")
	
	kubeconfigPath := "/root/.kube/config-37"
	kubeClient := GetKubeClient(kubeconfigPath)
	container := api.Container{Name: "ubuntu-container"}
	cmd := []string{"nvidia-smi"}
	pod := &v1.Pod{}
	pod.Namespace = "default"
	pod.Name = "gpu-pod"
	_, _, err := ExecCmdToPod(kubeClient, pod, container, cmd)
	if err != nil {
		fmt.Println(err)
		return
	}
}
