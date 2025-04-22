package mounter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/config"
	"github.com/coldzerofear/device-mounter/pkg/framework"
	"github.com/coldzerofear/device-mounter/pkg/util"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckPodContainerStatus Check if the specified container status is normal
func CheckPodContainerStatus(pod *v1.Pod, cont *api.Container) error {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == cont.Name {
			//if containerStatus.Started != nil && *containerStatus.Started {
			//	return nil
			//}
			if containerStatus.Ready {
				return nil
			}
		}
	}
	return fmt.Errorf("the target container %s is not ready", cont.Name)
}

// CheckPodContainer Check if the target container exists in the pod.
func CheckPodContainer(pod *v1.Pod, cont *api.Container) (*api.Container, error) {
	if pod == nil {
		return nil, api.NewMounterError(api.ResultCode_Invalid, "The target pod is empty")
	}
	if cont == nil {
		if len(pod.Spec.Containers) == 1 {
			ctr := &api.Container{}
			ctr.Name = pod.Spec.Containers[0].Name
			ctr.Index = 0
			return ctr, nil
		}
		msg := "Pod has multiple containers, target container must be specified"
		return nil, api.NewMounterError(api.ResultCode_Invalid, msg)
	}
	for i, container := range pod.Spec.Containers {
		checkSidecarFunc := func() error {
			if util.IsSidecar(container) {
				return api.NewMounterError(api.ResultCode_Invalid, "Target container is a sidecar container")
			}
			return nil
		}
		if container.Name == cont.Name {
			if err := checkSidecarFunc(); err != nil {
				return nil, err
			}
			ctr := &api.Container{}
			ctr.Name = cont.Name
			ctr.Index = uint32(i)
			return ctr, nil
		} else if cont.Name == "" && int(cont.Index) == i {
			if err := checkSidecarFunc(); err != nil {
				return nil, err
			}
			ctr := &api.Container{}
			ctr.Name = container.Name
			ctr.Index = cont.Index
			return ctr, nil
		}
	}
	msg := fmt.Sprintf("target container %v not found", cont)
	return nil, api.NewMounterError(api.ResultCode_Invalid, msg)
}

// CheckMountDeviceRequest Check if the device request is legitimate.
func CheckMountDeviceRequest(req *api.MountDeviceRequest) error {
	var paramNames []string
	if len(req.GetPodName()) == 0 {
		paramNames = append(paramNames, "'pod_name'")
	}
	if len(req.GetPodNamespace()) == 0 {
		paramNames = append(paramNames, "'pod_namespace'")
	}
	if req.GetResources() == nil || len(req.GetResources()) == 0 {
		paramNames = append(paramNames, "'resources'")
	}
	if len(paramNames) > 0 {
		msg := fmt.Sprintf("parameters [%s] cannot be empty", strings.Join(paramNames, ","))
		return api.NewMounterError(api.ResultCode_Invalid, msg)
	}
	// TODO If no container to be mounted is specified, index 0 is selected by default.
	if req.GetContainer() == nil {
		req.Container = &api.Container{Index: 0}
	}
	if req.GetAnnotations() == nil {
		req.Annotations = make(map[string]string)
	}
	if req.GetLabels() == nil {
		req.Labels = make(map[string]string)
	}
	return nil
}

func CheckUnMountDeviceRequest(req *api.UnMountDeviceRequest) error {
	var paramNames []string
	if len(req.GetPodName()) == 0 {
		paramNames = append(paramNames, "'pod_name'")
	}
	if len(req.GetPodNamespace()) == 0 {
		paramNames = append(paramNames, "'pod_namespace'")
	}
	if len(paramNames) > 0 {
		msg := fmt.Sprintf("parameters [%s] cannot be empty", strings.Join(paramNames, ","))
		return api.NewMounterError(api.ResultCode_Invalid, msg)
	}
	// TODO 没指定要卸载的容器，默认选择index0
	if req.GetContainer() == nil {
		req.Container = &api.Container{Index: 0}
	}
	return nil
}

// GarbageCollectionPods Batch delete pods
func GarbageCollectionPods(kubeClient *kubernetes.Clientset, objKeys []api.ObjectKey) []api.ObjectKey {
	var (
		err          error
		deleteFailed []api.ObjectKey
	)
	options := metav1.NewDeleteOptions(0)
	for i, objKey := range objKeys {
		klog.Infoln("Garbage collection pod", objKey.String())
		if objKey.UID != nil {
			options.Preconditions = metav1.NewUIDPreconditions(*objKey.UID)
		} else {
			options.Preconditions = nil
		}
		err = retry.OnError(retry.DefaultRetry, func(err error) bool {
			return !apierror.IsNotFound(err)
		}, func() error {
			return kubeClient.CoreV1().Pods(objKey.Namespace).Delete(context.Background(), objKey.Name, *options)
		})
		if apierror.IsNotFound(err) {
			err = nil
		}
		if err != nil {
			deleteFailed = append(deleteFailed, objKeys[i])
			klog.Errorf("GC pod %s failed: %v", objKey.String(), err)
		}
	}
	return deleteFailed
}

// WaitSupportPodsReady Waiting for the status of the created pods to be ready.
func WaitSupportPodsReady(ctx context.Context, podLister listerv1.PodLister,
	kubeClient *kubernetes.Clientset, deviceMounter framework.DeviceMounter,
	slavePodKeys []api.ObjectKey) ([]*v1.Pod, []*v1.Pod, error) {

	readySlavePods := make([]*v1.Pod, 0)
	skipSlavePods := make([]*v1.Pod, 0)
	condition := func(ctx context.Context) (bool, error) {
		for _, slaveKey := range slavePodKeys {
			//wait:
			slavePod, err := podLister.Pods(slaveKey.Namespace).Get(slaveKey.Name)
			if err != nil {
				if apierror.IsNotFound(err) {
					// When the target pod cannot be found in the cache,
					// it may be due to data synchronization delay that the target pod cannot be found in the cache.
					// Try to search for pod information through etcd. If none of them exist, throw an error.
					err = retry.OnError(retry.DefaultRetry, func(err error) bool { return !apierror.IsNotFound(err) }, func() error {
						slavePod, err = kubeClient.CoreV1().Pods(slaveKey.Namespace).Get(ctx, slaveKey.Name, metav1.GetOptions{})
						return err
					})
				}
				if err != nil {
					klog.V(3).ErrorS(err, "Get slave pod failed", "name", slaveKey.Name, "namespace", slaveKey.Namespace)
					return false, err
				}
			}
			statusCode, err := deviceMounter.VerifySupportPodStatus(ctx, slavePod.DeepCopy())
			switch statusCode {
			case api.Success:
				readySlavePods = append(readySlavePods, slavePod.DeepCopy())
				continue
			case api.Wait:
				// We will retry and reset the existing count.
				if len(readySlavePods) > 0 {
					readySlavePods = make([]*v1.Pod, 0)
				}
				if len(skipSlavePods) > 0 {
					skipSlavePods = make([]*v1.Pod, 0)
				}
				return false, nil
			case api.Skip:
				// readySlavePods[i] = slavePod.DeepCopy()
				skipSlavePods = append(skipSlavePods, slavePod.DeepCopy())
				continue
			case api.Unschedulable:
				if err == nil {
					err = fmt.Errorf("slave pod <%s> is not schedulable", slaveKey.String())
				}
				return true, err
			case api.Fail:
				if err == nil {
					err = fmt.Errorf("failed to verify slave pod <%s> status", slaveKey.String())
				} else if _, ok := err.(*api.MounterError); !ok {
					err = fmt.Errorf("failed to verify slave pod <%s> status: %v", slaveKey.String(), err)
				}
				return false, err
			default:
				return false, fmt.Errorf("the status return code of the position is incorrect: %v", statusCode)
			}
		}
		return true, nil
	}
	err := wait.PollUntilContextCancel(ctx, 100*time.Millisecond, false, condition)
	return readySlavePods, skipSlavePods, err
}

func Owner(pod *v1.Pod, controller bool) []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         "v1",
			Kind:               "Pod",
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			BlockOwnerDeletion: func(b bool) *bool { return &b }(true),
			Controller:         func(b bool) *bool { return &b }(controller),
		},
	}
}

func (s *DeviceMounterServer) GetSlavePods(devType string, ownerPod *v1.Pod, container *api.Container) ([]*v1.Pod, error) {
	selector := labels.SelectorFromSet(labels.Set{
		config.OwnerNameLabelKey:      ownerPod.Name,
		config.OwnerUidLabelKey:       string(ownerPod.UID),
		config.MountContainerLabelKey: container.Name,
	})
	pods, err := s.podLister.Pods(ownerPod.Namespace).List(selector)
	if err != nil {
		return nil, err
	}
	slavePods := make([]*v1.Pod, 0, len(pods))
	for _, pod := range pods {
		if pod.Annotations != nil && pod.Annotations[config.DeviceTypeAnnotationKey] == devType {
			slavePods = append(slavePods, pod.DeepCopy())
		} else {
			klog.V(4).Infoln("Skipped old slave pod:", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
		}
	}
	return slavePods, nil
}

func (s *DeviceMounterServer) CreatePodDisruptionBudget(ctx context.Context, ownerPod *v1.Pod) (*policyv1.PodDisruptionBudget, error) {
	pdb := policyv1.PodDisruptionBudget{}
	pdb.Name = ownerPod.Name
	pdb.Namespace = ownerPod.Namespace
	pdb.Labels = map[string]string{
		config.AppComponentLabelKey: config.CreateManagerBy,
		config.AppManagedByLabelKey: config.CreateManagerBy,
	}
	pdb.OwnerReferences = Owner(ownerPod, true)
	// TODO 确保至少有1个Pod副本在任何中断期间都是可用的 (防止资源泄漏)
	minAvailable := intstr.FromInt32(int32(1))
	pdb.Spec.MinAvailable = &minAvailable
	pdb.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			config.CreatedByLabelKey: ownerPod.Labels[config.CreatedByLabelKey],
		},
	}
	return s.kubeClient.PolicyV1().PodDisruptionBudgets(ownerPod.Namespace).Create(ctx, &pdb, metav1.CreateOptions{})
}

func (s *DeviceMounterServer) PatchPod(pod *v1.Pod, patches []string) (*v1.Pod, error) {
	if len(patches) == 0 {
		return pod, nil
	}
	klog.V(3).Infof("patching target pod patches: %+v", patches)
	marshalledPod, err := json.Marshal(pod)
	if err != nil {
		return pod, fmt.Errorf("JSON serialization failed: %v", err)
	}
	jsonPatch := "[\n" + strings.Join(patches, ",\n") + "\n]"
	patch, err := jsonpatch.DecodePatch([]byte(jsonPatch))
	if err != nil {
		return pod, fmt.Errorf("cannot decode pod patches %s: %v", jsonPatch, err)
	}
	modifiedMarshalledPod, err := patch.Apply(marshalledPod)
	if err != nil {
		return pod, fmt.Errorf("failed to apply patch for Pod %s: %v", jsonPatch, err)
	}
	patchedPod := &v1.Pod{}
	err = json.Unmarshal(modifiedMarshalledPod, patchedPod)
	if err != nil {
		return patchedPod, fmt.Errorf("cannot unmarshal modified marshalled Pod %s: %v",
			string(modifiedMarshalledPod), err)
	}
	klog.V(4).Infof("Patching target pod completed. Modified pod: %s",
		string(modifiedMarshalledPod))
	return patchedPod, nil
}

func (s *DeviceMounterServer) MutationPodFunc(devType string, container *api.Container, ownerPod, mutaPod *v1.Pod) {
	if mutaPod.Labels == nil {
		mutaPod.Labels = make(map[string]string)
	}
	if mutaPod.Spec.NodeSelector == nil {
		mutaPod.Spec.NodeSelector = make(map[string]string)
	}
	if mutaPod.Annotations == nil {
		mutaPod.Annotations = make(map[string]string)
	}
	mutaPod.DeletionTimestamp = nil
	mutaPod.Namespace = ownerPod.Namespace
	//mutaPod.Finalizers = []string{v1alpha1.Group + "/pod-protection"}
	getContainerId := func() string {
		for _, status := range ownerPod.Status.ContainerStatuses {
			if status.Name == container.Name {
				return status.ContainerID
			}
		}
		return ""
	}
	mutaPod.Annotations[config.DeviceTypeAnnotationKey] = devType
	mutaPod.Annotations[config.ContainerIdAnnotationKey] = getContainerId()

	mutaPod.Spec.NodeSelector[v1.LabelHostname] = s.nodeName

	mutaPod.Labels[config.CreatedByLabelKey] = uuid.New().String()
	mutaPod.Labels[config.OwnerNameLabelKey] = ownerPod.Name
	mutaPod.Labels[config.OwnerUidLabelKey] = string(ownerPod.UID)
	mutaPod.Labels[config.MountContainerLabelKey] = container.Name
	mutaPod.Labels[config.AppComponentLabelKey] = config.CreateManagerBy
	mutaPod.Labels[config.AppManagedByLabelKey] = config.CreateManagerBy
	// TODO (Don't set controller)
	// Batch schedulers like Volcano specify PodGroup for pods through OwnerReference.controller,
	// which can result in incorrect computation of PodGroup device resources.
	mutaPod.OwnerReferences = Owner(ownerPod, false)
	for i, _ := range mutaPod.Spec.Containers {
		mutaPod.Spec.Containers[i].Image = config.DeviceSlaveContainerImageTag
		mutaPod.Spec.Containers[i].ImagePullPolicy = config.DeviceSlaveImagePullPolicy
	}
	//mutaPod.Spec.Priority = nil
	//mutaPod.Spec.PriorityClassName = ownerPod.Spec.PriorityClassName
	mutaPod.Spec.TerminationGracePeriodSeconds = pointer.Int64(0)
}

func (s *DeviceMounterServer) GetCGroupPath(pod *v1.Pod, container *api.Container) (string, error) {
	var getFullPath func(string) string
	switch {
	case cgroups.IsCgroup2UnifiedMode(): // cgroupv2
		getFullPath = util.GetK8sPodCGroupFullPath
	case cgroups.IsCgroup2HybridMode():
		// If the device controller does not exist, use the path of cgroupv2.
		getFullPath = util.GetK8sPodDeviceCGroupFullPath
		if util.PathIsNotExist("/sys/fs/cgroup/devices") {
			getFullPath = util.GetK8sPodCGroupFullPath
		}
	default: // cgroupv1
		getFullPath = util.GetK8sPodDeviceCGroupFullPath
	}
	return util.GetK8sPodCGroupPath(pod, container, getFullPath)
}

func (s *DeviceMounterServer) DeviceRuleSetFunc(cgroupPath string, r *configs.Resources) (closed func() error, rollback func() error, err error) {
	closed = util.NilCloser
	rollback = util.NilCloser
	switch {
	case len(r.Devices) == 0:
		klog.V(3).Infoln("no device information to be mounted, skipping device permission settings")
	case cgroups.IsCgroup2UnifiedMode():
		klog.V(3).Infoln("use cgroupv2 ebpf device controller")
		closed, rollback, err = util.SetDeviceRulesByCgroupv2(cgroupPath, r)
	case cgroups.IsCgroup2HybridMode():
		// If the device controller does not exist, use cgroupv2.
		if util.PathIsNotExist("/sys/fs/cgroup/devices") {
			closed, rollback, err = util.SetDeviceRulesByCgroupv2(cgroupPath, r)
		} else {
			rollback, err = util.SetDeviceRulesByCgroupv1(cgroupPath, r)
		}
	default:
		rollback, err = util.SetDeviceRulesByCgroupv1(cgroupPath, r)
	}
	return
}

func (s *DeviceMounterServer) CreateDeviceFiles(cfg *util.Config, devInfos []api.DeviceInfo) (func() error, error) {
	if cfg == nil {
		return util.NilCloser, fmt.Errorf("nsenter config cannot be empty")
	}
	for _, devInfo := range devInfos {
		// TODO 暂且忽略设备文件创建失败的情况
		_ = util.AddDeviceFile(cfg, devInfo)
	}

	rollback := func() error {
		var err error
		for _, devInfo := range devInfos {
			if devInfo.Allow {
				devInfo.Allow = false
			}
			err = util.RemoveDeviceFile(cfg, devInfo)
		}
		return err
	}
	return rollback, nil
}

func (s *DeviceMounterServer) DeleteDeviceFiles(cfg *util.Config, devInfos []api.DeviceInfo) (func() error, error) {
	if cfg == nil {
		return util.NilCloser, fmt.Errorf("nsenter config cannot be empty")
	}
	for _, devInfo := range devInfos {
		// TODO 暂且忽略设备文件删除失败的情况
		_ = util.RemoveDeviceFile(cfg, devInfo)
	}

	rollback := func() error {
		var err error
		for _, devInfo := range devInfos {
			if !devInfo.Allow {
				devInfo.Allow = true
			}
			err = util.AddDeviceFile(cfg, devInfo)
		}
		return err
	}
	return rollback, nil
}

func (s *DeviceMounterServer) GetContainerCGroupPathAndPids(pod *v1.Pod, container *api.Container) ([]int, string, error) {
	cgroupPath, err := s.GetCGroupPath(pod, container)
	if err != nil {
		klog.V(4).ErrorS(err, "get cgroup path error")
		return nil, cgroupPath, err
	}
	klog.V(4).Infoln("current container cgroup path", cgroupPath)
	pids, err := cgroups.GetAllPids(cgroupPath)
	if err != nil {
		klog.V(4).ErrorS(err, "Get container pids error")
		return pids, cgroupPath, fmt.Errorf("error in obtaining container process id: %v", err)
	}
	if len(pids) == 0 {
		return pids, cgroupPath, fmt.Errorf("process id for target container not found")
	}
	return pids, cgroupPath, nil
}

func (s *DeviceMounterServer) GetTargetPod(ctx context.Context, name, namespace string) (*v1.Pod, error) {
	var (
		pod *v1.Pod
		err error
	)

	err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		return !apierror.IsNotFound(err)
	}, func() error {
		pod, err = s.kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{
			ResourceVersion: "0",
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	if pod.Spec.NodeName != s.nodeName {
		return nil, fmt.Errorf("target pod is not running on the node <%s>", s.nodeName)
	}
	klog.V(3).InfoS("Get target pod success", "pod", client.ObjectKeyFromObject(pod).String())
	return pod, nil
}
