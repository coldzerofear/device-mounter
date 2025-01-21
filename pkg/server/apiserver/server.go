package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/authConfig"
	"github.com/emicklei/go-restful/v3"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type APIService interface {
	MountDevice(request *restful.Request, response *restful.Response)
	UnMountDevice(request *restful.Request, response *restful.Response)
}

type mounterSelector struct {
	targetServerPort string
	targetNamespace  string
	labelSelector    labels.Selector
}

type service struct {
	*mounterSelector
	kubeClient *kubernetes.Clientset
	authConfig authConfig.Reader
}

func NewService(kubeClient *kubernetes.Clientset, authConfig authConfig.Reader,
	mounterPort, mounterNamespace string, mounterLabelSelector labels.Selector) (APIService, error) {
	if mounterLabelSelector == nil || mounterLabelSelector.Empty() {
		return nil, fmt.Errorf("The label selector of the device mounter cannot be empty")
	}
	return &service{
		mounterSelector: &mounterSelector{
			targetServerPort: mounterPort,
			targetNamespace:  mounterNamespace,
			labelSelector:    mounterLabelSelector,
		},
		kubeClient: kubeClient,
		authConfig: authConfig,
	}, nil
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
		Get(context.TODO(), params.name, metav1.GetOptions{
			ResourceVersion: "0",
		})
	if err != nil {
		if errors.IsNotFound(err) {
			_ = response.WriteError(http.StatusNotFound, fmt.Errorf("target pod does not exist: %w", err))
		} else {
			_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("error getting Pod: %w", err))
		}
		return
	}
	mPod, err := s.GetMounterPodOnNodeName(pod.Spec.NodeName)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}
	conn, err := grpc.Dial(mPod.Status.PodIP+s.targetServerPort, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
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
		PodName:      params.name,
		PodNamespace: params.namespace,
		Resources:    params.Resources,
		Annotations:  params.Annotations,
		Labels:       params.Labels,
		Container:    cont,
		DeviceType:   params.deviceType,
		Patches:      params.Patches,
	}
	timeout := time.Duration(params.timeoutSeconds) * time.Second
	ctx, cancelFunc := context.WithTimeout(request.Request.Context(), timeout)
	defer cancelFunc()
	resp, err := client.MountDevice(ctx, &req)
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

func getWaitTimeoutSecond(request *restful.Request) (int64, error) {
	timeout := int64(10)
	if timeoutParam := request.QueryParameter("wait_second"); timeoutParam != "" {
		var err error
		timeout, err = strconv.ParseInt(timeoutParam, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("failed to parse timeout: %w", err)
		}
		if timeout < 0 {
			return 0, fmt.Errorf("the timeout value can only be a positive integer")
		}
	}
	return timeout, nil
}

func readMountRequestParameters(request *restful.Request) (*requestMountParams, error) {
	namespace := strings.TrimSpace(request.PathParameter("namespace"))
	name := strings.TrimSpace(request.PathParameter("name"))
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name parameters are required")
	}
	container := strings.TrimSpace(request.QueryParameter("container"))
	devType := strings.TrimSpace(request.QueryParameter("device_type"))
	if devType == "" {
		return nil, fmt.Errorf("device_type parameters are required")
	}
	timeout, err := getWaitTimeoutSecond(request)
	if err != nil {
		return nil, err
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
	namespace := strings.TrimSpace(request.PathParameter("namespace"))
	name := strings.TrimSpace(request.PathParameter("name"))
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name parameters are required")
	}
	container := strings.TrimSpace(request.QueryParameter("container"))
	devType := strings.TrimSpace(request.QueryParameter("device_type"))
	if devType == "" {
		return nil, fmt.Errorf("device_type parameters are required")
	}
	timeout, err := getWaitTimeoutSecond(request)
	if err != nil {
		return nil, err
	}
	forceStr := request.QueryParameter("force")
	force := false
	if strings.ToLower(forceStr) == "true" {
		force = true
	}
	return &requestUnMountParams{
		name:           name,
		namespace:      namespace,
		container:      container,
		deviceType:     devType,
		timeoutSeconds: uint32(timeout),
		force:          force,
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
		Get(context.TODO(), params.name, metav1.GetOptions{
			ResourceVersion: "0",
		})
	if err != nil {
		if errors.IsNotFound(err) {
			_ = response.WriteError(http.StatusNotFound, fmt.Errorf("target pod does not exist: %w", err))
		} else {
			_ = response.WriteError(http.StatusInternalServerError, fmt.Errorf("error getting Pod: %w", err))
		}
		return
	}
	mPod, err := s.GetMounterPodOnNodeName(pod.Spec.NodeName)
	if err != nil {
		_ = response.WriteError(http.StatusInternalServerError, err)
		return
	}

	conn, err := grpc.Dial(mPod.Status.PodIP+s.targetServerPort, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
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
	timeout := time.Duration(params.timeoutSeconds) * time.Second
	ctx, cancelFunc := context.WithTimeout(request.Request.Context(), timeout)
	defer cancelFunc()
	resp, err := client.UnMountDevice(ctx, &req)
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
