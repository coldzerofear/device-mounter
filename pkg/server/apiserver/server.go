package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/emicklei/go-restful/v3"
	"google.golang.org/grpc"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/authConfig"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type APIService interface {
	MountDevice(request *restful.Request, response *restful.Response)
	UnMountDevice(request *restful.Request, response *restful.Response)
}

type service struct {
	port       string
	kubeClient *kubernetes.Clientset
	authConfig authConfig.Reader
}

func NewService(kubeClient *kubernetes.Clientset, authConfig authConfig.Reader, bindPort string) APIService {
	return &service{
		port:       bindPort,
		kubeClient: kubeClient,
		authConfig: authConfig,
	}
}

func (s *service) MountDevice(request *restful.Request, response *restful.Response) {
	klog.Infoln("Call MountDevice")
	params, err := readMountRequestParameters(request)
	if err != nil {
		_ = response.WriteError(http.StatusBadRequest, err)
		return
	}
	klog.V(4).Infoln("Request parameters", params)

	if err := s.check(request); err != nil {
		_ = response.WriteError(http.StatusBadRequest, err)
		return
	}

	pod, err := s.kubeClient.CoreV1().Pods(params.namespace).
		Get(context.TODO(), params.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_ = response.WriteError(http.StatusNotFound, fmt.Errorf("target pod does not exist: %w", err))
			return
		}
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("error getting Pod: %w", err))
		return
	}
	mPod, err := s.GetDeviceMounterPodOnNode(pod.Spec.NodeName)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}
	if mPod.Status.Phase != v1.PodRunning {
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("target device mounter not in running state"))
		return
	}
	conn, err := grpc.Dial(mPod.Status.PodIP+s.port, grpc.WithInsecure())
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("failed to connect to device mounter: %v", err))
		return
	}
	defer conn.Close()

	var cont *api.Container
	if len(params.container) > 0 {
		cont = &api.Container{
			Name: params.container,
		}
	}
	client := api.NewDeviceMountServiceClient(conn)
	req := api.MountDeviceRequest{
		PodName:        params.name,
		PodNamespace:   params.namespace,
		Resources:      params.Resources,
		Annotations:    params.Annotations,
		Container:      cont,
		DeviceType:     params.deviceType,
		TimeoutSeconds: params.timeoutSeconds,
		Patches:        params.Patches,
	}
	resp, err := client.MountDevice(request.Request.Context(), &req)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}

	if resp.Result == api.ResultCode_Success {
		_, _ = response.Write([]byte(resp.Message))
	} else {
		_ = response.WriteError(http.StatusBadRequest, fmt.Errorf(resp.Message))
	}
}

func readMountRequestParameters(request *restful.Request) (*requestMountParams, error) {
	namespace := request.PathParameter("namespace")
	name := request.PathParameter("name")
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name parameters are required")
	}
	container := request.QueryParameter("container")
	devType := request.QueryParameter("device_type")
	if len(devType) == 0 {
		return nil, fmt.Errorf("device_type parameters are required")
	}
	timeout := int64(10)
	if timeoutParam := request.QueryParameter("wait_second"); timeoutParam != "" {
		var err error
		timeout, err = strconv.ParseInt(timeoutParam, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timeout: %w", err)
		}
		if timeout < 0 {
			return nil, fmt.Errorf("the timeout value can only be a positive integer")
		}
	}
	body, err := io.ReadAll(request.Request.Body)
	if err != nil {
		return nil, err
	}
	defer request.Request.Body.Close()

	reqBody := requestMountBody{}
	if err = json.Unmarshal(body, &reqBody); err != nil {
		return nil, err
	}
	return &requestMountParams{
		name:             name,
		namespace:        namespace,
		container:        container,
		deviceType:       devType,
		requestMountBody: reqBody,
		timeoutSeconds:   uint32(timeout),
	}, nil
}

func readUnMountRequestParameters(request *restful.Request) (*requestUnMountParams, error) {
	namespace := request.PathParameter("namespace")
	name := request.PathParameter("name")
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name parameters are required")
	}
	container := request.QueryParameter("container")
	devType := request.QueryParameter("device_type")
	if len(devType) == 0 {
		return nil, fmt.Errorf("device_type parameters are required")
	}
	forceStr := request.QueryParameter("force")
	force := false
	if strings.ToLower(forceStr) == "true" {
		force = true
	}
	return &requestUnMountParams{
		name:       name,
		namespace:  namespace,
		container:  container,
		deviceType: devType,
		force:      force,
	}, nil
}

func (s *service) UnMountDevice(request *restful.Request, response *restful.Response) {
	klog.Infoln("Call MountDevice")

	params, err := readUnMountRequestParameters(request)
	if err != nil {
		_ = response.WriteError(http.StatusBadRequest, err)
		return
	}
	klog.V(4).Infoln("Request parameters", params)
	if err := s.check(request); err != nil {
		_ = response.WriteError(http.StatusBadRequest, err)
		return
	}
	pod, err := s.kubeClient.CoreV1().Pods(params.namespace).
		Get(context.TODO(), params.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_ = response.WriteError(http.StatusNotFound, fmt.Errorf("target pod does not exist: %w", err))
			return
		}
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("error getting Pod: %w", err))
		return
	}
	mPod, err := s.GetDeviceMounterPodOnNode(pod.Spec.NodeName)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}
	if mPod.Status.Phase != v1.PodRunning {
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("target device mounter not in running state"))
		return
	}
	conn, err := grpc.Dial(mPod.Status.PodIP+s.port, grpc.WithInsecure())
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("failed to connect to device mounter: %v", err))
		return
	}
	defer conn.Close()

	var cont *api.Container
	if len(params.container) > 0 {
		cont = &api.Container{Name: params.container}
	}
	client := api.NewDeviceMountServiceClient(conn)
	req := api.UnMountDeviceRequest{
		PodName:      params.name,
		PodNamespace: params.namespace,
		Container:    cont,
		Force:        params.force,
		DeviceType:   params.deviceType,
	}
	resp, err := client.UnMountDevice(request.Request.Context(), &req)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}
	if resp.Result == api.ResultCode_Success {
		_, _ = response.Write([]byte(resp.Message))
	} else {
		_ = response.WriteError(http.StatusBadRequest, fmt.Errorf(resp.Message))
	}
}
