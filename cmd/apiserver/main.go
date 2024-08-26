package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"

	"github.com/emicklei/go-restful/v3"
	"k8s-device-mounter/pkg/api/v1alpha1"
	"k8s-device-mounter/pkg/authConfig"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/filewatch"
	"k8s-device-mounter/pkg/server/apiserver"
	"k8s-device-mounter/pkg/tlsconfig"
	"k8s-device-mounter/pkg/versions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
)

const (
	serviceCertDir = "/tmp/k8s-device-mounter/serving-certs"
	certName       = "tls.crt"
	keyName        = "tls.key"

	configDir      = "/config"
	TlsProfileFile = "tls-profile-v1alpha1.yaml"
)

var (
	debug            bool
	version          bool
	KubeConfig       = ""
	TCPBindPort      = ":8768"
	MounterBindPort  = ":1200"
	MounterNamespace = "kube-system"
	MounterSelector  = ""
)

func initFlags() {
	klog.InitFlags(nil)
	flag.BoolVar(&debug, "debug", false, "Enable pprof endpoint (default false)")
	flag.BoolVar(&version, "version", false, "If true,query the version of the program (default false)")
	flag.StringVar(&KubeConfig, "kube-config", KubeConfig, "Load kubeconfig file location")
	flag.StringVar(&TCPBindPort, "tcp-bind-address", TCPBindPort, "TCP port bound to GRPC service (default \":8768\")")

	flag.StringVar(&MounterBindPort, "mounter-bind-address", MounterBindPort, "Device Mounter TCP port bound to GRPC service (default \":1200\")")
	flag.StringVar(&MounterNamespace, "mounter-pod-namespace", MounterNamespace, "The namespace of the device mounter pod (default \"kube-system\")")
	flag.StringVar(&MounterSelector, "mounter-label-selector", MounterSelector, "Specify the label selector for the device mounter pod")

}

func main() {
	initFlags()
	flag.Parse()
	if version {
		fmt.Printf("DeviceAPIServer version: %s \n", versions.AdjustVersion(versions.BuildVersion))
		return
	}

	kubeClient := client.GetKubeClient(KubeConfig)
	authConfigReader, err := authConfig.CreateReader(kubeClient.CoreV1().RESTClient())
	if err != nil {
		klog.Exitln(err)
	}
	defer authConfigReader.Stop()

	watch := filewatch.New()

	tlsConfigWatch := tlsconfig.NewWatch(
		configDir, TlsProfileFile,
		serviceCertDir, certName, keyName,
		authConfigReader,
	)
	tlsConfigWatch.Reload()

	if err = tlsConfigWatch.AddToFilewatch(watch); err != nil {
		klog.Exitln(err)
	}

	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		if err := watch.Run(watchDone); err != nil {
			klog.Errorf("Error running file watch: %s", err)
		}
	}()
	var selector labels.Selector
	if len(MounterSelector) > 0 {
		selector = apiserver.ParseLabelSelector(MounterSelector)
		klog.Infoln("Using custom label selectors to find device mounter", selector.String())
	} else {
		selector = labels.SelectorFromSet(labels.Set{
			config.AppComponentLabelKey: "daemonset",
			config.AppCreatedByLabelKey: "device-mounter-daemonset",
			config.AppInstanceLabelKey:  "device-mounter-daemonset",
		})
		klog.Infoln("Using default label selectors to find device mounter", selector.String())
	}
	handlers, err := apiserver.NewService(kubeClient, authConfigReader, MounterBindPort, MounterNamespace, selector)
	if err != nil {
		klog.Exitln(err)
	}
	webServer := apiServiceV1alpha1(handlers)
	if debug {
		klog.V(3).Infoln("enable pprof debugging information")
		routeFunction := func(handler func(http.ResponseWriter, *http.Request)) restful.RouteFunction {
			return func(request *restful.Request, response *restful.Response) {
				handler(response.ResponseWriter, request.Request)
			}
		}
		webServer.Route(webServer.GET("/pprof/profile").To(routeFunction(pprof.Profile)))
		webServer.Route(webServer.GET("/pprof/heap").To(routeFunction(pprof.Handler("heap").ServeHTTP)))
		webServer.Route(webServer.GET("/pprof/goroutine").To(routeFunction(pprof.Handler("goroutine").ServeHTTP)))
	}
	restful.Add(webServer)
	restful.Filter(restful.OPTIONSFilter())

	server := &http.Server{
		Addr: "0.0.0.0" + TCPBindPort,
		TLSConfig: &tls.Config{
			GetConfigForClient: func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
				config, err := tlsConfigWatch.GetConfig()
				if err == nil && debug {
					config.ClientAuth = tls.RequestClientCert
				}
				return config, err
			},
			GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
				// This function is not called, but it needs to be non-nil, otherwise
				// the server tries to load certificate from filenames passed to
				// ListenAndServe().
				panic("function should not be called")
			},
		},
	}

	if err = server.ListenAndServeTLS("", ""); err != nil {
		klog.Exitln(err)
	}
}

func apiServiceV1alpha1(handlers apiserver.APIService) *restful.WebService {
	ws := new(restful.WebService)

	// TODO 挂载设备
	ws.Route(ws.PUT("/apis/" + v1alpha1.GroupVersion.GroupVersion + "/namespaces/{namespace:[a-z0-9][a-z0-9\\-]*}/pods/{name:[a-z0-9][a-z0-9\\-]*}/mount").
		To(handlers.MountDevice).
		Doc("Mount device to container").
		Operation(v1alpha1.Version + "MountDevices").
		Consumes(restful.MIME_JSON).
		Param(ws.PathParameter("namespace", "The namespace of the target pod").Required(true)).
		Param(ws.PathParameter("name", "The name of the target pod").Required(true)).
		Param(ws.QueryParameter("device_type", "Mounted device resource types").Required(true)).
		Param(ws.QueryParameter("container", "The name of the target container").Required(false)).
		Param(ws.QueryParameter("wait_second", "Waiting for slave pod ready timeout (seconds)").
			Required(false).DataType("integer").DefaultValue("10")))

	// TODO 卸载设备
	ws.Route(ws.PUT("/apis/" + v1alpha1.GroupVersion.GroupVersion + "/namespaces/{namespace:[a-z0-9][a-z0-9\\-]*}/pods/{name:[a-z0-9][a-z0-9\\-]*}/unmount").
		To(handlers.UnMountDevice).
		Doc("UnMount device to container").
		Operation(v1alpha1.Version + "UnMountDevices").
		//Consumes(restful.MIME_JSON).
		Param(ws.PathParameter("namespace", "The namespace of the target pod").Required(true)).
		Param(ws.PathParameter("name", "The name of the target pod").Required(true)).
		Param(ws.QueryParameter("device_type", "Mounted device resource types").Required(true)).
		Param(ws.QueryParameter("container", "The name of the target container").Required(false)).
		Param(ws.QueryParameter("force", "Do you want to force device uninstallation").
			Required(false).DefaultValue("false")))

	// This endpoint is called by the API Server to get available resources.
	// 由k8s api-server调用得知当前server提供的服务
	ws.Route(ws.GET("/apis/"+v1alpha1.GroupVersion.GroupVersion).
		Produces(restful.MIME_JSON).Writes(metav1.APIResourceList{}).
		To(func(request *restful.Request, response *restful.Response) {
			list := &metav1.APIResourceList{
				TypeMeta: metav1.TypeMeta{
					Kind:       "APIResourceList",
					APIVersion: "v1",
				},
				GroupVersion: v1alpha1.GroupVersion.GroupVersion,
				APIResources: []metav1.APIResource{
					{
						Name:       "pods/mount",
						Namespaced: true,
					},
					{
						Name:       "pods/unmount",
						Namespaced: true,
					},
				},
			}
			response.WriteAsJson(list)
		}).
		Operation("GetAPIResources").
		Doc("Get API Resources").
		Returns(http.StatusOK, "OK", metav1.APIResourceList{}).
		Returns(http.StatusNotFound, "NotFound", ""))

	// K8s needs the ability to query info about a specific API group
	// 由k8s api-server调用得知当前的组信息
	ws.Route(ws.GET("/apis/"+v1alpha1.Group).
		Produces(restful.MIME_JSON).Writes(metav1.APIGroup{}).
		To(func(request *restful.Request, response *restful.Response) {
			response.WriteAsJson(v1alpha1.ApiGroup)
		}).
		Operation("GetSubAPIGroup").
		Doc("Get API Group").
		Returns(http.StatusOK, "OK", metav1.APIGroup{}).
		Returns(http.StatusNotFound, "NotFound", ""))

	// K8s needs the ability to query the list of API groups this endpoint supports
	// 由k8s api-server调用得知当前服务提供的组列表
	ws.Route(ws.GET("/apis").
		Produces(restful.MIME_JSON).Writes(metav1.APIGroupList{}).
		To(func(request *restful.Request, response *restful.Response) {
			response.WriteAsJson(v1alpha1.ApiGroupList)
		}).
		Operation("GetAPIGroupList").
		Doc("Get API Group List").
		Returns(http.StatusOK, "OK", metav1.APIGroupList{}).
		Returns(http.StatusNotFound, "NotFound", ""))

	// K8s needs the ability to query the root paths
	ws.Route(ws.GET("/").
		Produces(restful.MIME_JSON).Writes(metav1.RootPaths{}).
		To(func(request *restful.Request, response *restful.Response) {
			response.WriteAsJson(&metav1.RootPaths{
				Paths: []string{
					"/apis",
					"/apis/" + v1alpha1.Group,
					"/apis/" + v1alpha1.GroupVersion.GroupVersion,
				},
			})
		}).
		Operation("GetRootPaths").
		Doc("Get API Root Paths").
		Returns(http.StatusOK, "OK", metav1.RootPaths{}))

	return ws
}
