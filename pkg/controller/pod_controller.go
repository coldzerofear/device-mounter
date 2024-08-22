package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	client2 "k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type slavePodController struct {
	name             string
	client           *kubernetes.Clientset
	podLister        listerv1.PodLister
	podAddQueue      workqueue.RateLimitingInterface
	metadataFixQueue workqueue.RateLimitingInterface
	metadataCache    MetadataCache
}

var _ cache.ResourceEventHandler = &slavePodController{}

func NewPodController(name string, podInformer cache.SharedIndexInformer) *slavePodController {
	kubeConfig := client2.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)
	return &slavePodController{
		name:             name,
		client:           kubeClient,
		podLister:        listerv1.NewPodLister(podInformer.GetIndexer()),
		podAddQueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		metadataFixQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		metadataCache:    MetadataCache{},
	}
}

var (
	podLabelKeys = []string{
		config.OwnerNameLabelKey, config.OwnerUidLabelKey,
		config.CreatedByLabelKey, config.MountContainerLabelKey,
	}
	podAnnoKeys = []string{
		config.DeviceTypeAnnotationKey,
		config.ContainerIdAnnotationKey,
	}
)

func (c *slavePodController) OnAdd(obj interface{}, isInInitialList bool) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}
	if len(pod.Labels) == 0 || len(pod.Annotations) == 0 {
		return
	}
	containsKeys := func(keys []string, maps map[string]string) bool {
		for _, key := range keys {
			if _, ok := maps[key]; !ok {
				return false
			}
		}
		return true
	}
	if !containsKeys(podAnnoKeys, pod.Annotations) {
		return
	}
	if !containsKeys(podLabelKeys, pod.Labels) {
		return
	}
	if pod.Labels[config.AppComponentLabelKey] != config.CreateManagerBy {
		return
	}
	if pod.Labels[config.AppManagedByLabelKey] != config.CreateManagerBy {
		return
	}
	c.podAddQueue.Add(client.ObjectKeyFromObject(pod))
}

func (c *slavePodController) OnUpdate(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		return
	}
	if len(oldPod.Labels) == 0 || len(oldPod.Annotations) == 0 {
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		return
	}
	c.metadataFixEnqueue(oldPod, newPod)
}

func (c *slavePodController) metadataFixEnqueue(oldPod, newPod *v1.Pod) {
	// resync skip
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}
	metadata := Metadata{}
	if !reflect.DeepEqual(oldPod.Labels, newPod.Labels) {
		metadata.Labels = oldPod.Labels
	}
	if !reflect.DeepEqual(oldPod.Annotations, newPod.Annotations) {
		metadata.Annotations = oldPod.Annotations
	}
	if !reflect.DeepEqual(oldPod.OwnerReferences, newPod.OwnerReferences) {
		metadata.OwnerReferences = oldPod.OwnerReferences
	}
	if !reflect.DeepEqual(metadata, Metadata{}) {
		key := client.ObjectKeyFromObject(oldPod)
		c.metadataCache.Set(key, metadata)
		c.metadataFixQueue.Add(key)
	}
}

func (c *slavePodController) OnDelete(obj interface{}) {
	switch v := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		if pod, ok := v.Obj.(*v1.Pod); ok {
			c.metadataCache.Delete(client.ObjectKeyFromObject(pod))
		} else {
			splits := strings.Split(v.Key, string(types.Separator))
			if len(splits) == 2 {
				c.metadataCache.Delete(client.ObjectKey{
					Namespace: splits[0],
					Name:      splits[1],
				})
			}
		}
	case *v1.Pod:
		c.metadataCache.Delete(client.ObjectKeyFromObject(v))
	default:
		klog.V(4).Infof("OnDelete(obj) unknow type: %v", v)
	}
}

//func (c *slavePodController) Start(ctx context.Context, workerNum int) {
//	for i := 0; i < workerNum; i++ {
//		go wait.Until(func() {for processNextWorkItem(ctx, c.podAddQueue, c.handlePodAdd) {}}, time.Second, ctx.Done())
//		go wait.Until(func() {for processNextWorkItem(ctx, c.podAddQueue, c.handleMetadataFix) {}}, time.Second, ctx.Done())
//	}
//}

func (c *slavePodController) Start(ctx context.Context, workerNum int) {
	go func() {
		<-ctx.Done()
		klog.Infoln(c.name, "is stopping...")
		c.podAddQueue.ShutDown()
		c.metadataFixQueue.ShutDown()
	}()
	wg := &sync.WaitGroup{}
	wg.Add(workerNum * 2)
	for i := 0; i < workerNum; i++ {
		go func() {
			defer wg.Done()
			for processNextWorkItem(ctx, c.podAddQueue, c.handlePodAdd) {
			}
		}()
		go func() {
			defer wg.Done()
			for processNextWorkItem(ctx, c.metadataFixQueue, c.handleMetadataFix) {
			}
		}()
	}
	wg.Wait()
	klog.Infoln(c.name, "stopped")
}

func processNextWorkItem[request comparable](ctx context.Context, queue workqueue.RateLimitingInterface, workerFunc func(context.Context, request) (reconcile.Result, error)) (loop bool) {
	defer func() {
		if rec := recover(); rec != nil {
			klog.Errorln("processNextWorkItem panic", rec)
			loop = true
		}
	}()
	obj, shutdown := queue.Get()
	if shutdown {
		return false
	}
	defer queue.Done(obj)

	// Make sure that the object is a valid request.
	req, ok := obj.(request)
	if !ok {
		// As the item in the workqueue is actually invalid, we call
		// Forget here else we'd go into a loop of attempting to
		// process a work item that is invalid.
		queue.Forget(obj)
		// Return true, don't take a break
		return true
	}

	klog.V(5).Infoln("Reconciling")
	result, err := workerFunc(ctx, req)
	switch {
	case err != nil:
		queue.AddRateLimited(req)
		if !result.IsZero() {
			klog.Info("Warning: Reconciler returned both a non-zero result and a non-nil error. The result will always be ignored if the error is non-nil and the non-nil error causes reqeueuing with exponential backoff. For more details, see: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Reconciler")
		}
		klog.Error(err, "Reconciler error")
	case result.RequeueAfter > 0:
		klog.V(5).Info(fmt.Sprintf("Reconcile done, requeueing after %s", result.RequeueAfter))
		// The result.RequeueAfter request will be lost, if it is returned
		// along with a non-nil error. But this is intended as
		// We need to drive to stable reconcile loops before queuing due
		// to result.RequestAfter
		queue.Forget(obj)
		queue.AddAfter(req, result.RequeueAfter)
	case result.Requeue:
		klog.V(5).Info("Reconcile done, requeueing")
		queue.AddRateLimited(req)
	default:
		klog.V(5).Info("Reconcile successful")
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		queue.Forget(obj)
	}
	return true
}

func (c *slavePodController) handlePodAdd(ctx context.Context, req client.ObjectKey) (reconcile.Result, error) {
	klog.V(3).Infof("handle add pod %s", req.String())
	return reconcile.Result{}, nil
}

func (c *slavePodController) handleMetadataFix(ctx context.Context, req client.ObjectKey) (rs reconcile.Result, err error) {
	klog.V(3).Infof("metadata fix pod %s", req.String())
	var pod *v1.Pod
	rs = reconcile.Result{}
	pod, err = c.podLister.Pods(req.Namespace).Get(req.Name)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.V(3).ErrorS(err, "get pod failed", "podKey", req.String())
		} else {
			err = nil
		}
		return
	}
	if !pod.DeletionTimestamp.IsZero() {
		klog.V(4).Infof("Pod %s has been marked for deletion, Skip metadata fix", req.String())
		return
	}
	metadata, ok := c.metadataCache.Get(req)
	if !ok {
		return
	}
	defer func() {
		// 处理完毕
		if err == nil {
			c.metadataCache.Done(req, metadata)
		}
	}()

	newPod := pod.DeepCopy()
	comparableKeys := func(old, new, result map[string]string, keys []string) {
		if len(old) == 0 {
			return
		}
		for _, key := range keys {
			val, ok := old[key]
			if !ok {
				continue
			}
			if new == nil || new[key] != val {
				result[key] = val
			}
		}
	}
	// 修复labels
	labels := make(map[string]string)
	comparableKeys(metadata.Labels, newPod.Labels, labels, podLabelKeys)
	util.CopyMap(labels, newPod.Labels)

	// 修复annotations
	annos := make(map[string]string)
	comparableKeys(metadata.Annotations, newPod.Annotations, annos, podAnnoKeys)
	util.CopyMap(annos, newPod.Annotations)

	// 修复OwnerReferences
	if len(metadata.OwnerReferences) > 0 {
		newPod.OwnerReferences = metadata.OwnerReferences
	}

	if !reflect.DeepEqual(pod.ObjectMeta, newPod.ObjectMeta) {
		var patchData []byte
		patch := client.StrategicMergeFrom(pod)
		patchData, err = patch.Data(newPod)
		if err != nil {
			klog.V(3).ErrorS(err, "StrategicMerge patch data error")
			return
		}
		_, err = c.client.CoreV1().Pods(req.Namespace).Patch(ctx, req.Name,
			patch.Type(), patchData, metav1.PatchOptions{}, "")
		if err != nil {
			if !errors.IsNotFound(err) {
				klog.V(3).ErrorS(err, "patch pod failed", "name", pod.Name, "namespace", pod.Namespace)
			} else {
				err = nil
			}
		} else {
			klog.V(4).Infof("Successfully fix pod %s metadata", req.String())
		}
	}
	return
}
