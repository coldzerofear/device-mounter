package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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

func NewPodController(name string, podInformer cache.SharedIndexInformer) *slavePodController {
	kubeConfig := client2.GetKubeConfig("")
	kubeClient, _ := kubernetes.NewForConfig(kubeConfig)
	return &slavePodController{
		name:           name,
		client:         kubeClient,
		podLister:      listerv1.NewPodLister(podInformer.GetIndexer()),
		podAddQueue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		podUpdateQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		updateCache:    updateCache{},
	}
}

type UpdateRequest struct {
	Labels          map[string]string       `json:"labels,omitempty"`
	Annotations     map[string]string       `json:"annotations,omitempty"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences,omitempty"`
}

type updateCache struct {
	sync.Map
}

func (u *updateCache) Done(key string, old UpdateRequest) {
	data, _ := json.Marshal(old)
	u.CompareAndDelete(key, string(data))
}

func (u *updateCache) Set(key string, obj UpdateRequest) {
	data, _ := json.Marshal(obj)
	_, ok := u.LoadOrStore(key, string(data))
	if ok {
		u.Store(key, string(data))
	}
}

func (u *updateCache) Get(key string) (UpdateRequest, bool) {
	load, ok := u.Load(key)
	update := UpdateRequest{}
	if ok {
		_ = json.Unmarshal([]byte(load.(string)), &update)
	}
	return update, ok
}

type slavePodController struct {
	name           string
	client         *kubernetes.Clientset
	podLister      listerv1.PodLister
	podAddQueue    workqueue.RateLimitingInterface
	podUpdateQueue workqueue.RateLimitingInterface
	updateCache    updateCache
}

var _ cache.ResourceEventHandler = &slavePodController{}

func (c *slavePodController) OnAdd(obj interface{}, isInInitialList bool) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}
	_, ok = pod.Annotations[config.DeviceTypeAnnotationKey]
	if !ok {
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

var (
	podLabelKeys = []string{config.OwnerNameLabelKey, config.OwnerUidLabelKey,
		config.CreatedByLabelKey, config.MountContainerLabelKey}
	podAnnoKeys = []string{config.DeviceTypeAnnotationKey}
)

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
	// resync skip
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}
	if newPod.Labels == nil {
		newPod.Labels = map[string]string{}
	}
	if newPod.Annotations == nil {
		newPod.Annotations = map[string]string{}
	}
	comparableKeys := func(old, new, result map[string]string, keys []string) {
		for _, key := range keys {
			val, ok := old[key]
			if !ok {
				continue
			}
			if new[key] != val {
				result[key] = val
			}
		}
	}
	labels := make(map[string]string)
	comparableKeys(oldPod.Labels, newPod.Labels, labels, podLabelKeys)
	annos := make(map[string]string)
	comparableKeys(oldPod.Annotations, newPod.Annotations, annos, podAnnoKeys)
	var references []metav1.OwnerReference
	if !reflect.DeepEqual(oldPod.OwnerReferences, newPod.OwnerReferences) {
		references = oldPod.OwnerReferences
	}
	if len(labels) == 0 && len(annos) == 0 && len(references) == 0 {
		return
	}
	objKey := client.ObjectKeyFromObject(newPod)
	c.updateCache.Set(objKey.String(), UpdateRequest{
		Labels:          labels,
		Annotations:     annos,
		OwnerReferences: references,
	})
	c.podUpdateQueue.Add(objKey)
}

func (c *slavePodController) OnDelete(obj interface{}) {
	switch v := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		if pod, ok := v.Obj.(*v1.Pod); ok {
			c.updateCache.Delete(client.ObjectKeyFromObject(pod).String())
		} else {
			c.updateCache.Delete(v.Key)
		}
	case *v1.Pod:
		c.updateCache.Delete(client.ObjectKeyFromObject(v).String())
	default:
		klog.V(4).Infof("OnDelete(obj) unknow type: %v", v)
	}
}

func (c *slavePodController) Start(ctx context.Context, workerNum int) {
	go func() {
		<-ctx.Done()
		klog.Infoln(c.name, "is stopping...")
		c.podAddQueue.ShutDown()
		c.podUpdateQueue.ShutDown()
	}()
	wg := &sync.WaitGroup{}
	wg.Add(workerNum * 2)
	for i := 0; i < workerNum; i++ {
		//go wait.Until(func() {
		//	for processNextWorkItem(ctx, c.podAddQueue, c.handlePodAdd) {
		//	}
		//}, time.Second, ctx.Done())
		go func() {
			defer wg.Done()
			for processNextWorkItem(ctx, c.podAddQueue, c.handlePodAdd) {
			}
		}()
		go func() {
			defer wg.Done()
			for processNextWorkItem(ctx, c.podUpdateQueue, c.handlePodUpdateAdapter) {
			}
		}()
	}
	wg.Wait()
	klog.Infoln(c.name, "stopped")
}

func processNextWorkItem[T any](ctx context.Context, queue workqueue.RateLimitingInterface, workerFunc func(context.Context, T) (reconcile.Result, error)) (loop bool) {
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
	req, ok := obj.(T)
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
	klog.V(3).Infof("add pod %s", req.String())
	return reconcile.Result{}, nil
}

func (c *slavePodController) handlePodUpdateAdapter(ctx context.Context, req client.ObjectKey) (rs reconcile.Result, err error) {
	klog.V(3).Infof("update pod %s", req.String())
	rs = reconcile.Result{}
	pod, err := c.podLister.Pods(req.Namespace).Get(req.Name)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.V(3).ErrorS(err, "get pod failed", "podKey", req.String())
		} else {
			err = nil
		}
		return rs, err
	}
	update, ok := c.updateCache.Get(req.String())
	if !ok {
		return rs, err
	}
	defer func() {
		if err == nil {
			c.updateCache.Done(req.String(), update)
		}
	}()
	err = c.handlePodUpdate(ctx, pod, update)
	return rs, err
}

func (c *slavePodController) handlePodUpdate(ctx context.Context, pod *v1.Pod, update UpdateRequest) error {
	targetPod := pod.DeepCopy()
	util.CopyMap(update.Labels, targetPod.Labels)
	util.CopyMap(update.Annotations, targetPod.Annotations)
	if update.OwnerReferences != nil {
		targetPod.OwnerReferences = update.OwnerReferences
	}
	if !reflect.DeepEqual(pod.ObjectMeta, targetPod.ObjectMeta) {
		patchData, err := client.StrategicMergeFrom(pod).Data(targetPod)
		if err != nil {
			return err
		}
		_, err = c.client.CoreV1().Pods(targetPod.Namespace).Patch(ctx, targetPod.Name,
			types.StrategicMergePatchType, patchData, metav1.PatchOptions{}, "")
		if err != nil {
			if !errors.IsNotFound(err) {
				klog.V(3).ErrorS(err, "patch pod failed", "name", pod.Name, "namespace", pod.Namespace)
			} else {
				err = nil
			}
			return err
		}
	}
	return nil
}
