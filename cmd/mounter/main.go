package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	"google.golang.org/grpc"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/devices"
	"k8s-device-mounter/pkg/server/mounter"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	cache2 "sigs.k8s.io/controller-runtime/pkg/cache"
)

var (
	TCPBindPort = ":1200"
	SocketPath  = "/var/run/k8s-device-mounter"
	KubeConfig  = ""
)

func initFlags() {
	klog.InitFlags(nil)
	flag.StringVar(&TCPBindPort, "tcp-bind-address", TCPBindPort, "TCP port bound to GRPC service (default :1200)")
	flag.StringVar(&KubeConfig, "kube-config", KubeConfig, "Load kubeconfig file location")
	flag.StringVar(&SocketPath, "socket-path", SocketPath, "Specify the directory where the socket file is located (default /var/run/k8s-device-mounter)")

	flag.StringVar(&config.DeviceSlaveContainerImageTag, "device-slave-image-tag", config.DeviceSlaveContainerImageTag, "Specify the image tag for the slave container (default alpine:latest)")
	flag.StringVar((*string)(&config.DeviceSlaveImagePullPolicy), "device-slave-pull-policy", string(config.DeviceSlaveImagePullPolicy), "Specify the image pull policy for the slave container (default IfNotPresent)")
	//	flag.StringVar(&config.DeviceSlavePodNamespace, "device-slave-namespace", config.DeviceSlavePodNamespace, "Specify the namespace of the slave container (default device-pool)")

}

func main() {
	initFlags()
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	if strings.TrimSpace(nodeName) == "" {
		klog.Exit("Unknown node name, please configure environment variables [NODE_NAME]")
	}

	klog.Infoln("Initialize cgroup driver...")
	util.InitCGroupDriver()

	klog.Infoln("Initialize the pod resources client...")
	// TODO 初始化客户端
	proxyClient := client.GetPodResourcesClinet()
	defer proxyClient.Close()

	klog.Infoln("Initialize the kube client...")
	kubeClient := client.GetKubeClient(KubeConfig)

	// 创建命名空间
	//klog.Infoln("Create slave pod namespace...")
	//err := createSlaveNamesapce(kubeClient, config.DeviceSlavePodNamespace)
	//if err != nil {
	//	klog.Exit(err.Error())
	//}

	klog.Infoln("Initialize the informer factory...")
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 2*time.Minute)
	informerFactory.Core().V1().Pods().Informer()
	podInformer := informerFactory.InformerFor(&v1.Pod{},
		func(k kubernetes.Interface, duration time.Duration) cache.SharedIndexInformer {
			return cache.NewSharedIndexInformer(cache.NewListWatchFromClient(
				k.CoreV1().RESTClient(), "pods", metav1.NamespaceAll,
				fields.OneTermEqualSelector("spec.nodeName", nodeName)), &v1.Pod{},
				duration, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
		})
	_ = podInformer.SetTransform(cache2.TransformStripManagedFields())
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	klog.Infoln("Initialize the event recorder...")
	kubeConfig := client.GetKubeConfig(KubeConfig)
	evnetClient, _ := kubernetes.NewForConfig(kubeConfig)
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: evnetClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "DeviceMounter"})

	serverImpl := &mounter.DeviceMounterImpl{
		NodeName:   nodeName,
		KubeClient: kubeClient,
		Recorder:   recorder,
		PodLister:  listerv1.NewPodLister(podInformer.GetIndexer()),
		IsCGroupV2: cgroups.IsCgroup2UnifiedMode(),
	}

	// 注册设备挂载器
	klog.Infoln("Register Device Mounter...")
	if err := devices.RegisrtyDeviceMounter(); err != nil {
		klog.Exit(err.Error())
	}
	var deviceTypes []string
	for _, mount := range devices.RegisterDeviceMounter {
		deviceTypes = append(deviceTypes, mount.DeviceType())
	}
	klog.Infoln("Successfully registered mounts include", deviceTypes)

	klog.Infoln("Service Starting...")

	// TODO 启动tpc服务
	stopCh1 := make(chan struct{}, 1)
	s1, err := StartTcpService(serverImpl, stopCh1)
	if err != nil {
		klog.Exit(err.Error())
	}

	// TODO 启动unix服务
	stopCh2 := make(chan struct{}, 1)
	s2, err := StartUnixService(serverImpl, stopCh2)
	if err != nil {
		klog.Exit(err.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGQUIT)

	select {
	case <-stopCh1:
		klog.Infoln("The grpc tpc service has been shut down.")
		klog.Infoln("Shutting down grpc unix service...")
		s2.GracefulStop()

	case <-stopCh2:
		klog.Infoln("The grpc unix service has been shut down.")
		klog.Infoln("Shutting down grpc tcp service...")
		s1.GracefulStop()

	case s := <-sigChan:
		klog.Infof("Received signal %v, shutting down.", s)
		klog.Infoln("Shutting down grpc tcp service...")
		s1.GracefulStop()
		klog.Infoln("Shutting down grpc unix service...")
		s2.GracefulStop()
	}
	klog.Infoln("Service stopped, please restart the service")
	os.Exit(1)
}

func createSlaveNamesapce(kubeClient *kubernetes.Clientset, namespace string) error {
	ns := v1.Namespace{}
	ns.Name = namespace
	ns.SetLabels(map[string]string{
		"app.kubernetes.io/created-by": "k8s-device-mounter",
		"app.kubernetes.io/managed-by": "k8s-device-mounter",
	})
	_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
	if err != nil {
		klog.V(3).Infof("Create slave pod namespace failed: %v", err)
		if errors.IsAlreadyExists(err) {
			err = nil
		}
	}
	return err
}

func StartTcpService(server api.DeviceMountServiceServer, stopCh chan<- struct{}) (*grpc.Server, error) {
	listen, err := net.Listen("tcp", TCPBindPort)
	if err != nil {
		klog.Errorf("Failed to listen: %v", err)
		return nil, err
	}
	s := grpc.NewServer()
	api.RegisterDeviceMountServiceServer(s, server)
	klog.Infoln("Serving tcp server...")
	go func() {
		defer listen.Close()
		if err = s.Serve(listen); err != nil {
			klog.ErrorS(err, "TCP grpc error")
			stopCh <- struct{}{}
		}
	}()
	return s, nil
}

func StartUnixService(server api.DeviceMountServiceServer, stopCh chan<- struct{}) (*grpc.Server, error) {
	socketFile := filepath.Join(SocketPath, "device-mounter.sock")
	_ = os.Remove(socketFile)
	addr, err := net.ResolveUnixAddr("unix", socketFile)
	if err != nil {
		klog.Errorf("Failed to resolve unix addr: %v", err)
		return nil, err
	}
	listen, err := net.ListenUnix("unix", addr)
	if err != nil {
		klog.Errorf("Failed to listen: %v", err)
		return nil, err
	}
	s := grpc.NewServer()
	api.RegisterDeviceMountServiceServer(s, server)
	klog.Infoln("Serving unix server...")
	go func() {
		defer func() {
			_ = listen.Close()
			_ = os.Remove(socketFile)
		}()
		if err = s.Serve(listen); err != nil {
			klog.ErrorS(err, "Unix grpc error")
			stopCh <- struct{}{}
		}
	}()
	return s, nil
}
