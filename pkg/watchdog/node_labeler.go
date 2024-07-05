package watchdog

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"gomodules.xyz/jsonpatch/v2"
	"k8s-device-mounter/pkg/api/v1alpha1"
	"k8s-device-mounter/pkg/framework"
	"k8s-device-mounter/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	SchedulingLabelPrefix = "scheduling." + v1alpha1.Group
)

type nodeLabeler struct {
	nodeName   string
	kubeClient *kubernetes.Clientset
	done       atomic.Bool
	stopped    chan struct{}
	listerv1.NodeLister
}

func NewNodeLabeler(nodeName string, nodeInformer cache.SharedIndexInformer, kubeClient *kubernetes.Clientset) *nodeLabeler {
	lister := listerv1.NewNodeLister(nodeInformer.GetIndexer())
	return &nodeLabeler{
		nodeName:   nodeName,
		kubeClient: kubeClient,
		NodeLister: lister,
		stopped:    make(chan struct{}, 1),
	}
}

func ContainsDeviceTypes(labelKey string) bool {
	for devType, _ := range framework.RegisterDeviceMounter {
		if labelKey == strings.ToLower(SchedulingLabelPrefix+"/"+devType) {
			return true
		}
	}
	return false
}

func (l *nodeLabeler) Done() {
	if l.done.Load() {
		return
	}
	<-l.stopped
	close(l.stopped)
	l.done.Swap(true)
}

func (l *nodeLabeler) Start(stopCh <-chan struct{}) {
	if l.done.Load() {
		klog.Errorf("The NodeLabeler has been Done and cannot be restarted")
		return
	}
	for {
		select {
		case <-stopCh:
			_ = l.cleanupLabels()
			klog.Infoln("NodeLabeler has stopped")
			l.stopped <- struct{}{}
			return
		default:
			if l.updateLabels() != nil {
				time.Sleep(5 * time.Second)
			} else {
				time.Sleep(15 * time.Second)
			}
		}
	}
}

func (l *nodeLabeler) cleanupLabels() error {
	node, err := l.Get(l.nodeName)
	if err != nil {
		klog.V(3).ErrorS(err, "NodeLabeler Error Searching for Current Node")
		return err
	}

	var patches []jsonpatch.Operation
	for labelKey, _ := range node.Labels {
		if !strings.HasPrefix(labelKey, SchedulingLabelPrefix) {
			continue
		}
		newKey := strings.ReplaceAll(labelKey, "/", "~1")
		patches = append(patches, jsonpatch.Operation{
			Operation: "remove", Path: "/metadata/labels/" + newKey, Value: nil,
		})
	}
	if len(patches) > 0 {
		klog.V(3).Infoln("Patch node labels", patches)
		jsonPatch, err := json.Marshal(patches)
		if err != nil {
			klog.V(3).ErrorS(err, "Serializing JSON failed")
			return err
		}
		_, err = l.kubeClient.CoreV1().Nodes().Patch(context.Background(),
			l.nodeName, types.JSONPatchType, jsonPatch, metav1.PatchOptions{})
		if err != nil {
			klog.V(3).ErrorS(err, "NodeLabeler patch labels error")
			return err
		}
	}
	return nil
}

func (l *nodeLabeler) updateLabels() error {
	node, err := l.Get(l.nodeName)
	if err != nil {
		klog.V(3).ErrorS(err, "NodeLabeler Error Searching for Current Node")
		return err
	}
	var (
		labelKeys   []string
		addKeys     []string
		removeKeys  []string
		replaceKeys []string
	)

	var patches []jsonpatch.Operation
	for labelKey, _ := range node.Labels {
		if !strings.HasPrefix(labelKey, SchedulingLabelPrefix) {
			continue
		}

		labelKeys = append(labelKeys, labelKey)
		if !ContainsDeviceTypes(labelKey) {
			removeKeys = append(removeKeys, labelKey)
		}
	}

	for devType, _ := range framework.RegisterDeviceMounter {
		labelKey := strings.ToLower(SchedulingLabelPrefix + "/" + devType)
		if !util.ContainsString(labelKeys, labelKey) {
			addKeys = append(addKeys, labelKey)
		} else if node.Labels[labelKey] != "true" {
			replaceKeys = append(replaceKeys, labelKey)
		}
	}
	for _, key := range addKeys {
		newKey := strings.ReplaceAll(key, "/", "~1")
		patches = append(patches, jsonpatch.Operation{
			Operation: "add", Path: "/metadata/labels/" + newKey, Value: "true",
		})
	}
	for _, key := range removeKeys {
		newKey := strings.ReplaceAll(key, "/", "~1")
		patches = append(patches, jsonpatch.Operation{
			Operation: "remove", Path: "/metadata/labels/" + newKey, Value: nil,
		})
	}
	for _, key := range replaceKeys {
		newKey := strings.ReplaceAll(key, "/", "~1")
		patches = append(patches, jsonpatch.Operation{
			Operation: "replace", Path: "/metadata/labels/" + newKey, Value: "true",
		})
	}
	if len(patches) > 0 {
		klog.V(3).Infoln("Patch node labels", patches)
		jsonPatch, err := json.Marshal(patches)
		if err != nil {
			klog.V(3).ErrorS(err, "Serializing JSON failed")
			return err
		}
		_, err = l.kubeClient.CoreV1().Nodes().Patch(context.Background(),
			l.nodeName, types.JSONPatchType, jsonPatch, metav1.PatchOptions{})
		if err != nil {
			klog.V(3).ErrorS(err, "NodeLabeler patch labels error")
			return err
		}
	}
	return nil
}
