package client

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/coldzerofear/device-mounter/pkg/versions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func userAgent() string {
	return fmt.Sprintf(
		"%s/%s (%s/%s) kubernetes/%s",
		versions.AdjustCommand(os.Args[0]),
		versions.AdjustVersion(versions.BuildVersion),
		runtime.GOOS, runtime.GOARCH,
		versions.AdjustCommit(versions.BuildCommit))
}

var (
	initKubeConfig sync.Once
	kubeConfig     *rest.Config
)

func InitKubeConfig(masterUrl, kubeconfig string) error {
	var err error
	initKubeConfig.Do(func() {
		kubeConfig, err = clientcmd.BuildConfigFromFlags(masterUrl, kubeconfig)
	})
	return err
}

func GetKubeConfig(opts ...Option) (*rest.Config, error) {
	err := InitKubeConfig("", "")
	if err != nil {
		return nil, err
	}
	config := rest.CopyConfig(kubeConfig)
	for _, opt := range opts {
		opt(config)
	}
	return config, nil
}

type Option func(*rest.Config)

func GetClientSet(opts ...Option) (*kubernetes.Clientset, error) {
	config, err := GetKubeConfig(opts...)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func WithQPS(qps float32, burst int) Option {
	return func(config *rest.Config) {
		config.QPS = qps
		config.Burst = burst
	}
}

func WithDefaultUserAgent() Option {
	return WithUserAgent(userAgent())
}

func WithUserAgent(userAgent string) Option {
	return func(config *rest.Config) {
		config.UserAgent = userAgent
	}
}

func WithDefaultContentType() Option {
	return WithContentType(
		"application/vnd.kubernetes.protobuf,application/json",
		"application/json")
}

func WithContentType(acceptContentTypes, contentType string) Option {
	return func(config *rest.Config) {
		config.AcceptContentTypes = acceptContentTypes
		config.ContentType = contentType
	}
}
