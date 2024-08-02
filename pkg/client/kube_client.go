package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	defaultQPS         = float32(100)
	defaultBurst       = int(defaultQPS * 2)
	defaultTimeout     = 10 * time.Second
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
	kubeConfig.Timeout = defaultTimeout
	kubeConfig.UserAgent = userAgent()
	kubeConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	kubeConfig.ContentType = "application/vnd.kubernetes.protobuf"
	kubeClient, err = kubernetes.NewForConfig(kubeConfig)
	return err
}

func userAgent() string {
	return fmt.Sprintf(
		"%s/%s (%s/%s) kubernetes/%s",
		adjustCommand(os.Args[0]),
		adjustVersion(version.Get().GitVersion),
		runtime.GOOS, runtime.GOARCH,
		adjustCommit(version.Get().GitCommit))
}

func adjustCommand(p string) string {
	// Unlikely, but better than returning "".
	if len(p) == 0 {
		return "unknown"
	}
	return filepath.Base(p)
}

func adjustVersion(v string) string {
	if len(v) == 0 {
		return "unknown"
	}
	seg := strings.SplitN(v, "-", 2)
	return seg[0]
}

func adjustCommit(c string) string {
	if len(c) == 0 {
		return "unknown"
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
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
	kubeConfig.QPS = 0
	kubeConfig.Burst = 0
	return config
}

func RetryGetPodByName(kubeClient *kubernetes.Clientset, name, namespace string, retryCount uint) (*v1.Pod, error) {
	var pod *v1.Pod
	err := util.LoopRetry(retryCount, 100*time.Millisecond, func() (bool, error) {
		var err1 error
		pod, err1 = kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err1 != nil {
			if apierror.IsNotFound(err1) {
				return false, err1
			}
			return false, nil
		}
		return true, nil
	})
	return pod, err
}

func PatchPodAnnotations(kubeclient *kubernetes.Clientset, pod *v1.Pod, annotations map[string]string) error {
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
		Patch(context.Background(), pod.Name, types.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("patch pod %v failed, %v", pod.Name, err)
	}
	return err
}
