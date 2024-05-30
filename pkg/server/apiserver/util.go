package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/emicklei/go-restful/v3"
	"k8s-device-mounter/pkg/config"
	authzv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

type requestMountBody struct {
	Resources   map[string]string `json:"resources"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type requestMountParams struct {
	requestMountBody
	container      string
	deviceType     string
	name           string
	namespace      string
	timeoutSeconds uint32
}

type requestUnMountParams struct {
	name       string
	namespace  string
	container  string
	deviceType string
	force      bool
}

func (s *service) GetDeviceMounterPodOnNode(nodeName string) (*v1.Pod, error) {
	selector := labels.SelectorFromSet(labels.Set{
		config.AppComponentLabelKey: "daemonset",
		config.AppCreatedByLabelKey: "device-mounter-daemonset",
		config.AppInstanceLabelKey:  "device-mounter-daemonset",
	})
	podList, err := s.kubeClient.CoreV1().Pods("kube-system").
		List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("error getting device mounter Pod: %w", err)
	}
	// 根据node name找到对应的daemon
	var mPod *v1.Pod
	for _, mounterPod := range podList.Items {
		if mounterPod.Spec.NodeName == nodeName && mounterPod.Status.Phase == v1.PodRunning {
			for _, status := range mounterPod.Status.ContainerStatuses {
				if !status.Ready {
					return nil, fmt.Errorf("the device mounter is not ready on the target node %s", nodeName)
				}
			}
			mPod = mounterPod.DeepCopy()
			break
		}
	}
	if mPod == nil {
		return nil, fmt.Errorf("there is no device mounter on the target node %s", nodeName)
	}
	return mPod, nil
}

func (s *service) check(request *restful.Request) error {
	requestHeader := request.Request.Header

	user, err := s.getAuthUsername(requestHeader)
	if err != nil {
		return err
	}
	klog.V(4).Infoln("Visiting users", user)
	groups, err := s.getAuthGroups(requestHeader)
	if err != nil {
		return err
	}
	klog.V(4).Infoln("Auth groups", groups)

	if _, err := s.getAuthExtraHeaders(requestHeader); err != nil {
		return err
	}

	return nil
}

func (s *service) getAuthUsername(requestHeader http.Header) (string, error) {
	userHeaders, err := s.authConfig.GetUserHeaders()
	if err != nil {
		return "", err
	}

	for _, header := range userHeaders {
		if usernames, ok := requestHeader[header]; ok && len(usernames) > 0 {
			return usernames[0], nil
		}
	}

	return "", fmt.Errorf("a valid user header is required")
}

func (s *service) getAuthGroups(requestHeader http.Header) ([]string, error) {
	groupHeaders, err := s.authConfig.GetGroupHeaders()
	if err != nil {
		return nil, err
	}

	var groups []string
	var foundHeader bool
	for _, header := range groupHeaders {
		if vals, ok := requestHeader[header]; ok {
			foundHeader = true
			groups = append(groups, vals...)
		}
	}

	if !foundHeader {
		return nil, fmt.Errorf("a valid group header is required")
	}
	return groups, nil
}

func (s *service) getAuthExtraHeaders(requestHeader http.Header) (map[string]authzv1.ExtraValue, error) {
	extraHeaderPrefixes, err := s.authConfig.GetExtraHeaderPrefixes()
	if err != nil {
		return nil, err
	}

	extras := map[string]authzv1.ExtraValue{}

outerLoop:
	for key, values := range requestHeader {
		for _, prefix := range extraHeaderPrefixes {
			if strings.HasPrefix(key, prefix) {
				extraKey := strings.TrimPrefix(key, prefix)
				extras[extraKey] = values
				continue outerLoop
			}
		}
	}

	return extras, nil
}
