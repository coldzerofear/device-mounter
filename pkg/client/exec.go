package client

import (
	"bytes"
	"fmt"

	"k8s-device-mounter/pkg/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/cp"
	"k8s.io/kubectl/pkg/cmd/util"
)

func ExecCmdToPod(kubeClient *kubernetes.Clientset, pod *v1.Pod, ctr *api.Container, cmd []string) (string, string, error) {
	// 执行命令
	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("container", ctr.Name).
		VersionedParams(&v1.PodExecOptions{
			Command:   cmd,
			Container: ctr.Name,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	config := GetKubeConfig(kubeConfigPath)
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	var stdout, stderr bytes.Buffer
	err = executor.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil {
		// Error executing command: unable to upgrade connection: you must specify at least 1 of stdin, stdout, stderr
		return "", "", fmt.Errorf("Error executing command: %v", err)
	}
	// 输出命令执行结果
	fmt.Println("STDOUT:", stdout.String())
	fmt.Println("STDERR:", stderr.String())
	return stdout.String(), stderr.String(), nil
}

func CopyToPod(kubeClient *kubernetes.Clientset, pod *v1.Pod, ctr *api.Container, src, dst string) (string, string, error) {
	kubeconfig := GetKubeConfig(kubeConfigPath)
	kubeconfig.APIPath = "/api"
	kubeconfig.GroupVersion = &schema.GroupVersion{Version: "v1"} // this targets the core api groups so the url path will be /api/v1
	kubeconfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}

	f := util.NewFactory(genericclioptions.NewConfigFlags(true))
	ioStreams, _, stdout, stderr := genericiooptions.NewTestIOStreams()
	cmd := cp.NewCmdCp(f, ioStreams)
	opts := cp.NewCopyOptions(ioStreams)
	args := []string{src, fmt.Sprintf("%s/%s:%s", pod.Namespace, pod.Name, dst)}
	if err := opts.Complete(f, cmd, args); err != nil {
		return "", "", err
	}
	opts.ClientConfig = kubeconfig
	opts.Clientset = kubeClient
	opts.Namespace = pod.Namespace
	opts.Container = ctr.Name
	if err := opts.Run(); err != nil {
		return "", "", err
	}
	fmt.Println("STDOUT:", stdout.String())
	fmt.Println("STDERR:", stderr.String())
	return stdout.String(), stderr.String(), nil
}
