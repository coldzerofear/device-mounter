package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	"google.golang.org/grpc"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/controller"
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/server/mounter"
	"k8s-device-mounter/pkg/versions"
	"k8s-device-mounter/pkg/watchdog"

	// init device mounter
	_ "k8s-device-mounter/pkg/devices"
	v1 "k8s.io/api/core/v1"
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
	version     bool
	TCPBindPort = ":1200"
	SocketPath  = "/var/run/k8s-device-mounter"
	KubeConfig  = ""
)

func initFlags() {
	klog.InitFlags(nil)
	flag.BoolVar(&version, "version", false, "If true,query the version of the program (default false)")
	flag.StringVar(&TCPBindPort, "tcp-bind-address", TCPBindPort, "TCP port bound to GRPC service (default :1200)")
	flag.StringVar(&KubeConfig, "kube-config", KubeConfig, "Load kubeconfig file location")
	flag.StringVar(&SocketPath, "socket-path", SocketPath, "Specify the directory where the socket file is located (default /var/run/k8s-device-mounter)")

	flag.StringVar(&config.DeviceSlaveContainerImageTag, "device-slave-image-tag", config.DeviceSlaveContainerImageTag, "Specify the image tag for the slave container (default alpine:latest)")
	flag.StringVar((*string)(&config.DeviceSlaveImagePullPolicy), "device-slave-pull-policy", string(config.DeviceSlaveImagePullPolicy), "Specify the image pull policy for the slave container (default IfNotPresent)")
}

func main() {
	initFlags()
	flag.Parse()
	if version {
		fmt.Printf("DeviceMounter version: %s \n", versions.AdjustVersion(versions.BuildVersion))
		return
	}

	nodeName := os.Getenv("NODE_NAME")
	if strings.TrimSpace(nodeName) == "" {
		klog.Exit("Unknown node name, please configure environment variables [NODE_NAME]")
	}

	klog.Infoln("Initialize CGroup driver...")
	config.InitCGroupDriver()

	klog.Infoln("Initialize the pod resources client...")
	// TODO 初始化客户端
	proxyClient := client.GetPodResourcesClinet()
	defer proxyClient.Close()

	klog.Infoln("Initialize the kube client...")
	kubeClient := client.GetKubeClient(KubeConfig)

	klog.Infoln("Initialize the informer factory...")
	resyncConfig := informers.WithCustomResyncConfig(map[metav1.Object]time.Duration{
		&v1.Node{}: 30 * time.Minute, &v1.Pod{}: 5 * time.Minute,
	})
	transform := informers.WithTransform(cache2.TransformStripManagedFields())
	informerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, 0, resyncConfig, transform)
	podInformer := initPodInformer(informerFactory, nodeName)
	_ = podInformer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		klog.Errorf("pod informer watch error: %v", err)
	})
	podController := controller.NewPodController("PodController", podInformer)
	_, _ = podInformer.AddEventHandler(podController)
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	_ = nodeInformer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		klog.Errorf("node informer watch error: %v", err)
	})
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
	ctx, cancelFunc := context.WithCancel(context.TODO())
	go podController.Start(ctx, 1)

	klog.Infoln("Initialize the event recorder...")
	kubeConfig := client.GetKubeConfig(KubeConfig)
	eventClient, _ := kubernetes.NewForConfig(kubeConfig)
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: eventClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "DeviceMounter"})

	nodeLister := listerv1.NewNodeLister(nodeInformer.GetIndexer())
	podLister := listerv1.NewPodLister(podInformer.GetIndexer())
	serverImpl := &mounter.DeviceMounterImpl{
		NodeName:   nodeName,
		KubeClient: kubeClient,
		Recorder:   recorder,
		NodeLister: nodeLister,
		PodLister:  podLister,
		IsCGroupV2: cgroups.IsCgroup2UnifiedMode(),
	}

	// 注册设备挂载器
	klog.Infoln("Registering Device Mounter...")
	if err := framework.RegisrtyDeviceMounter(); err != nil {
		klog.Exit(err.Error())
	}
	deviceTypes := framework.GetDeviceMounterTypes()
	klog.Infoln("Successfully registered mounts include", deviceTypes)

	klog.Infoln("Watchdog Starting...")
	kubeConfig = client.GetKubeConfig(KubeConfig)
	nodeClient, _ := kubernetes.NewForConfig(kubeConfig)
	nodeLabeller := watchdog.NewNodeLabeller(nodeName, nodeLister, nodeClient)
	go nodeLabeller.Start(ctx.Done())

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
	exitCode := 0
	select {
	case <-stopCh1:
		klog.Infoln("The grpc tpc service has been shut down.")
		klog.Infoln("Shutting down grpc unix service...")
		s2.GracefulStop()
		exitCode = 1
	case <-stopCh2:
		klog.Infoln("The grpc unix service has been shut down.")
		klog.Infoln("Shutting down grpc tcp service...")
		s1.GracefulStop()
		exitCode = 1
	case s := <-sigChan:
		klog.Infof("Received signal %v, shutting down.", s)
		klog.Infoln("Shutting down grpc tcp service...")
		s1.GracefulStop()
		klog.Infoln("Shutting down grpc unix service...")
		s2.GracefulStop()
	}
	cancelFunc() // 关闭controller
	nodeLabeller.WaitForStop()
	klog.Infoln("Service stopped, please restart the service")
	os.Exit(exitCode)
}

func initPodInformer(factory informers.SharedInformerFactory, nodeName string) cache.SharedIndexInformer {
	return factory.InformerFor(&v1.Pod{}, func(k kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			cache.NewListWatchFromClient(
				k.CoreV1().RESTClient(),
				"pods",
				metav1.NamespaceAll,
				fields.OneTermEqualSelector("spec.nodeName", nodeName),
			),
			&v1.Pod{},
			resyncPeriod,
			cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			})
	})
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
