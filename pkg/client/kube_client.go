package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/coldzerofear/device-mounter/pkg/versions"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	defaultQPS         = float32(100)
	defaultBurst       = int(defaultQPS * 2)
	defaultTimeout     = 30 * time.Second
	onceInitKubeClient sync.Once
	kubeConfigPath     string
	kubeConfig         *rest.Config
	kubeClient         *kubernetes.Clientset
)

func initConfigAndClient(kubeconfigPath string) error {
	var err error
	kubeConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err
	}
	kubeConfig.QPS = defaultQPS
	kubeConfig.Burst = defaultBurst
	//kubeConfig.Timeout = defaultTimeout
	kubeConfig.UserAgent = userAgent()
	kubeConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	kubeConfig.ContentType = "application/vnd.kubernetes.protobuf"
	kubeClient, err = kubernetes.NewForConfig(kubeConfig)
	return err
}

func userAgent() string {
	return fmt.Sprintf(
		"%s/%s (%s/%s) kubernetes/%s",
		versions.AdjustCommand(os.Args[0]),
		versions.AdjustVersion(versions.BuildVersion),
		runtime.GOOS, runtime.GOARCH,
		versions.AdjustCommit(versions.BuildCommit))
}

func GetKubeClient(kubeconfigPath string) *kubernetes.Clientset {
	onceInitKubeClient.Do(func() {
		kubeConfigPath = kubeconfigPath
		if err := initConfigAndClient(kubeconfigPath); err != nil {
			klog.Exitf("K8s Client initialization failed: %v", err)
		}
	})
	return kubeClient
}

func GetKubeConfig(kubeconfigPath string) *rest.Config {
	onceInitKubeClient.Do(func() {
		kubeConfigPath = kubeconfigPath
		if err := initConfigAndClient(kubeConfigPath); err != nil {
			klog.Exitf("K8s Config initialization failed: %v", err)
		}
	})
	config := rest.CopyConfig(kubeConfig)
	kubeConfig.Timeout = defaultTimeout
	kubeConfig.QPS = 0
	kubeConfig.Burst = 0
	return config
}

func PatchPodAnnotations(ctx context.Context, kubeclient *kubernetes.Clientset, pod *v1.Pod, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kubeclient.CoreV1().Pods(pod.Namespace).
		Patch(ctx, pod.Name, types.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("patch pod %v failed, %v", pod.Name, err)
	}
	return err
}
