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

	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/client"
	"github.com/coldzerofear/device-mounter/pkg/config"
	"github.com/coldzerofear/device-mounter/pkg/controller"
	"github.com/coldzerofear/device-mounter/pkg/framework"
	"github.com/coldzerofear/device-mounter/pkg/server/mounter"
	"github.com/coldzerofear/device-mounter/pkg/versions"
	"github.com/coldzerofear/device-mounter/pkg/watchdog"
	"google.golang.org/grpc"

	// init device mounter
	_ "github.com/coldzerofear/device-mounter/pkg/devices"
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
	SocketPath  = "/var/run/device-mounter"
	KubeConfig  = ""
)

func initFlags() {
	klog.InitFlags(nil)
	flag.BoolVar(&version, "version", false, "If true,query the version of the program (default false)")
	flag.StringVar(&TCPBindPort, "tcp-bind-address", TCPBindPort, "TCP port bound to GRPC service (default :1200)")
	flag.StringVar(&KubeConfig, "kube-config", KubeConfig, "Load kubeconfig file location")
	flag.StringVar(&SocketPath, "socket-path", SocketPath, "Specify the directory where the socket file is located (default /var/run/device-mounter)")

	flag.StringVar(&config.DeviceSlaveContainerImageTag, "device-slave-image-tag", config.DeviceSlaveContainerImageTag, "Specify the image tag for the slave container (default alpine:latest)")
	flag.StringVar((*string)(&config.DeviceSlaveImagePullPolicy), "device-slave-pull-policy", string(config.DeviceSlaveImagePullPolicy), "Specify the image pull policy for the slave container (default IfNotPresent)")
	flag.Parse()
}

func printVersionInfo() {
	if version {
		fmt.Printf("DeviceMounter version: %s \n", versions.AdjustVersion(versions.BuildVersion))
		os.Exit(0)
	}
}

func newEventRecorder() record.EventRecorderLogger {
	klog.Infoln("Initialize the event recorder...")
	kubeConfig := client.GetKubeConfig(KubeConfig)
	eventClient, _ := kubernetes.NewForConfig(kubeConfig)
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: eventClient.CoreV1().Events("")})
	return broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "DeviceMounter"})
}

func main() {
	initFlags()
	printVersionInfo()

	nodeName := os.Getenv("NODE_NAME")
	if strings.TrimSpace(nodeName) == "" {
		klog.Exit("Unknown node name, please configure environment variables [NODE_NAME]")
	}

	klog.Infoln("Initialize CGroup driver...")
	config.InitCGroupDriver()

	klog.Infoln("Initialize the pod resources client...")
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
	podInformer := newPodInformer(informerFactory, nodeName)
	_ = podInformer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		klog.Errorf("pod informer watch error: %v", err)
	})
	podController := controller.NewPodController("PodController", podInformer)
	if _, err := podInformer.AddEventHandler(podController); err != nil {
		klog.Exit("AddEventHandler failed")
	}
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	_ = nodeInformer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		klog.Errorf("node informer watch error: %v", err)
	})
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
	ctx, cancelFunc := context.WithCancel(context.TODO())
	go podController.Start(ctx, 1)

	nodeLister := listerv1.NewNodeLister(nodeInformer.GetIndexer())
	podLister := listerv1.NewPodLister(podInformer.GetIndexer())
	serverImpl := mounter.NewDeviceMounterServer(nodeName, kubeClient, podLister, nodeLister, newEventRecorder())

	klog.Infoln("Registering Device Mounter...")
	if err := framework.RegisrtyDeviceMounter(); err != nil {
		klog.Exit(err.Error())
	}
	deviceTypes := framework.GetDeviceMounterTypes()
	klog.Infoln("Successfully registered mounts include", deviceTypes)

	klog.Infoln("Watchdog Starting...")
	kubeConfig := client.GetKubeConfig(KubeConfig)
	nodeClient, _ := kubernetes.NewForConfig(kubeConfig)
	nodeLabeller := watchdog.NewNodeLabeller(nodeName, nodeLister, nodeClient)
	go nodeLabeller.Start(ctx.Done())

	klog.Infoln("Service Starting...")

	stopCh1 := make(chan struct{}, 1)
	s1, err := StartTcpService(serverImpl, stopCh1)
	if err != nil {
		klog.Exit(err.Error())
	}

	stopCh2 := make(chan struct{}, 1)
	s2, err := StartUnixService(serverImpl, stopCh2)
	if err != nil {
		klog.Exit(err.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT,
		syscall.SIGTERM, syscall.SIGKILL, syscall.SIGQUIT)
	exitCode := 0
	// Use service graceful stop to prevent unexpected interruption of ongoing task requests.
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
	cancelFunc()
	nodeLabeller.WaitForStop()
	klog.Infoln("Service stopped, please restart the service")
	os.Exit(exitCode)
}

func newPodInformer(factory informers.SharedInformerFactory, nodeName string) cache.SharedIndexInformer {
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
