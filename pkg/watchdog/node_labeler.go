package watchdog

import (
	"context"
	"encoding/json"
	"strings"
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

type nodeLabeler struct {
	nodeName   string
	kubeClient *kubernetes.Clientset
	listerv1.NodeLister
}

func NewNodeLabeler(nodeName string, nodeInformer cache.SharedIndexInformer, kubeClient *kubernetes.Clientset) *nodeLabeler {
	lister := listerv1.NewNodeLister(nodeInformer.GetIndexer())
	return &nodeLabeler{
		nodeName:   nodeName,
		kubeClient: kubeClient,
		NodeLister: lister,
	}
}

func ContainsDeviceTypes(labelKey string) bool {
	for devType, _ := range framework.RegisterDeviceMounter {
		if labelKey == strings.ToLower(v1alpha1.Group+"/"+devType) {
			return true
		}
	}
	return false
}

func (l *nodeLabeler) Start() {
	for {
		node, err := l.Get(l.nodeName)
		if err != nil {
			klog.V(3).ErrorS(err, "NodeLabeler Error Searching for Current Node")
			time.Sleep(time.Second)
			continue
		}
		var (
			labelKeys   []string
			addKeys     []string
			removeKeys  []string
			replaceKeys []string
		)

		var patches []jsonpatch.Operation
		for key, _ := range node.Labels {
			if !strings.HasPrefix(key, v1alpha1.Group) {
				continue
			}

			labelKeys = append(labelKeys, key)
			if !ContainsDeviceTypes(key) {
				removeKeys = append(removeKeys, key)
			}
		}

		for devType, _ := range framework.RegisterDeviceMounter {
			labelKey := strings.ToLower(v1alpha1.Group + "/" + devType)
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
				time.Sleep(5 * time.Second)
				continue
			}
			_, err = l.kubeClient.CoreV1().Nodes().Patch(context.Background(),
				l.nodeName, types.JSONPatchType, jsonPatch, metav1.PatchOptions{})
			if err != nil {
				klog.V(3).ErrorS(err, "NodeLabeler patch labels error")
				time.Sleep(5 * time.Second)
				continue
			}
		}
		time.Sleep(30 * time.Second)
	}
}
