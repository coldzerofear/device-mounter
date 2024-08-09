package controller

import (
	"encoding/json"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MetadataCache struct {
	cache sync.Map
}

type Metadata struct {
	Labels          map[string]string       `json:"labels,omitempty"`
	Annotations     map[string]string       `json:"annotations,omitempty"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences,omitempty"`
}

func (cache *MetadataCache) Delete(key client.ObjectKey) {
	cache.cache.Delete(key.String())
}

func (cache *MetadataCache) Done(key client.ObjectKey, metadata Metadata) {
	data, _ := json.Marshal(metadata)
	cache.cache.CompareAndDelete(key.String(), string(data))
}

func (cache *MetadataCache) Set(key client.ObjectKey, metadata Metadata) {
	data, _ := json.Marshal(metadata)
	_, ok := cache.cache.LoadOrStore(key.String(), string(data))
	if ok {
		cache.cache.Store(key, string(data))
	}
}

func (cache *MetadataCache) Get(key client.ObjectKey) (Metadata, bool) {
	load, ok := cache.cache.Load(key.String())
	metadata := Metadata{}
	if ok {
		_ = json.Unmarshal([]byte(load.(string)), &metadata)
	}
	return metadata, ok
}
