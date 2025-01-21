package client

import (
	"bytes"
	"context"
	"fmt"

	"github.com/coldzerofear/device-mounter/pkg/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/cp"
	"k8s.io/kubectl/pkg/cmd/util"
)

func WriteToPod(ctx context.Context, kubeclient *kubernetes.Clientset,
	pod *v1.Pod, ctr *api.Container, content []byte, cmd []string) (string, string, error) {
	req := kubeclient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("container", ctr.Name).
		VersionedParams(&v1.PodExecOptions{
			Command:   cmd,
			Container: ctr.Name,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	config := GetKubeConfig(kubeConfigPath)
	executor, err := remotecommand.
		NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	buf := bytes.NewBuffer(content)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = executor.StreamWithContext(
		ctx,
		remotecommand.StreamOptions{
			Stdin:  buf,
			Stdout: stdout,
			Stderr: stderr,
			Tty:    false,
		})
	if err != nil {
		return "", "", err
	}
	// 输出命令执行结果
	printOutput(stdout, stderr)
	return stdout.String(), stderr.String(), nil
}

func ExecCmdToPod(ctx context.Context, kubeclient *kubernetes.Clientset,
	pod *v1.Pod, ctr *api.Container, cmd []string) (string, string, error) {
	// 执行命令
	req := kubeclient.CoreV1().RESTClient().Post().
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
	executor, err := remotecommand.
		NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = executor.StreamWithContext(
		ctx,
		remotecommand.StreamOptions{
			Stdin:  nil,
			Stdout: stdout,
			Stderr: stderr,
			Tty:    false,
		})
	if err != nil {
		return "", "", err
	}
	// 输出命令执行结果
	printOutput(stdout, stderr)
	return stdout.String(), stderr.String(), nil
}

func CopyToPod(kubeclient *kubernetes.Clientset, pod *v1.Pod,
	ctr *api.Container, src, dst string) (string, string, error) {
	kubeconfig := GetKubeConfig(kubeConfigPath)
	kubeconfig.APIPath = "/api"
	// this targets the core api groups so the url path will be /api/v1
	kubeconfig.GroupVersion = &schema.GroupVersion{Version: "v1"}
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
	opts.Clientset = kubeclient
	opts.Namespace = pod.Namespace
	opts.Container = ctr.Name
	//opts.MaxTries = 3
	if err := opts.Run(); err != nil {
		return "", "", err
	}
	printOutput(stdout, stderr)
	return stdout.String(), stderr.String(), nil
}

func printOutput(stdout, stderr *bytes.Buffer) {
	if stdout != nil && stdout.String() != "" {
		klog.Infoln("STDOUT:", stdout.String())
	}
	if stderr != nil && stderr.String() != "" {
		klog.Errorln("STDERR:", stderr.String())
	}
}
