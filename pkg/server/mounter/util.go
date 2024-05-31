package mounter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opencontainers/runc/libcontainer/configs"
	"k8s-device-mounter/pkg/api"
	"k8s-device-mounter/pkg/client"
	"k8s-device-mounter/pkg/config"
	"k8s-device-mounter/pkg/devices"
	"k8s-device-mounter/pkg/util"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

func CheckPodContainerStatus(pod *v1.Pod, cont *api.Container) error {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == cont.Name {
			if containerStatus.Started != nil && *containerStatus.Started {
				return nil
			}
		}
	}
	return fmt.Errorf("The target container %s is not running", cont.Name)
}

func CheckPodContainer(pod *v1.Pod, cont *api.Container) (*api.Container, error) {
	if pod == nil {
		return nil, fmt.Errorf("The target pod is empty")
	}
	if cont == nil {
		if len(pod.Spec.Containers) == 1 {
			ctr := &api.Container{}
			ctr.Name = pod.Spec.Containers[0].Name
			ctr.Index = 0
			return ctr, nil
		}
		return nil, fmt.Errorf("Pod has multiple containers, target container must be specified")
	}
	for i, container := range pod.Spec.Containers {
		if container.Name == cont.Name {
			ctr := &api.Container{}
			ctr.Name = cont.Name
			ctr.Index = uint32(i)
			return ctr, nil
		} else if cont.Name == "" && int(cont.Index) == i {
			ctr := &api.Container{}
			ctr.Name = container.Name
			ctr.Index = cont.Index
			return ctr, nil
		}
	}
	return nil, fmt.Errorf("Target container %v not found", cont)
}

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
		return fmt.Errorf("Parameters %s cannot be empty", strings.Join(paramNames, ","))
	}
	// TODO 没指定要挂载的容器，默认选择index0
	if req.GetContainer() == nil {
		req.Container = &api.Container{Index: 0}
	}
	if req.GetAnnotations() == nil {
		req.Annotations = make(map[string]string)
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
		return fmt.Errorf("Parameters %s cannot be empty", strings.Join(paramNames, ","))
	}
	// TODO 没指定要卸载的容器，默认选择index0
	if req.GetContainer() == nil {
		req.Container = &api.Container{Index: 0}
	}
	return nil
}

func RecyclingPods(ctx context.Context, kubeClient *kubernetes.Clientset, slavePodKeys []types.NamespacedName) error {
	var err error
	for _, objKey := range slavePodKeys {
		klog.Infoln("Recycling pod", objKey.String())
		// TODO 暂时忽略删除失败 （设备泄漏风险）
		if err1 := kubeClient.CoreV1().Pods(objKey.Namespace).
			Delete(ctx, objKey.Name, metav1.DeleteOptions{
				GracePeriodSeconds: pointer.Int64(0),
			}); err1 != nil {
			err = err1
		}
	}
	return err
}

func WaitSlavePodsReady(ctx context.Context,
	podLister listerv1.PodLister, kubeClient *kubernetes.Clientset,
	deviceMounter devices.DeviceMounterInterface, timeoutSecond time.Duration,
	slavePodKeys []types.NamespacedName) ([]*v1.Pod, []*v1.Pod, api.ResultCode, error) {

	//readySlavePods := make([]*v1.Pod, len(slavePodNames))

	readySlavePods := make([]*v1.Pod, 0)
	skipSlavePods := make([]*v1.Pod, 0)
	resCode := api.ResultCode_Fail
	condition := func(ctx context.Context) (bool, error) {
		for _, slaveKey := range slavePodKeys {
			//wait:
			slavePod, err := podLister.Pods(slaveKey.Namespace).Get(slaveKey.Name)
			if err != nil {
				if apierror.IsNotFound(err) {
					// 当本地缓存找不到则从api-server处查询
					slavePod, err = client.RetryGetPodByName(kubeClient, slaveKey.Name, slaveKey.Namespace, 3)
				}
				if err != nil {
					klog.V(3).ErrorS(err, "Get slave pod failed")
					return false, err
				}
			}
			statusCode, err := deviceMounter.CheckDeviceSlavePodStatus(slavePod.DeepCopy())
			switch statusCode {
			case api.Success:
				//readySlavePods[i] = slavePod.DeepCopy()
				readySlavePods = append(readySlavePods, slavePod.DeepCopy())
				continue
			case api.Wait:
				//time.Sleep(100 * time.Millisecond)
				//goto wait

				// 等待将进行重试
				// 重置已有计数
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
				resCode = api.ResultCode_Insufficient
				return true, err
			case api.Fail:
				// 抛出错误
				return false, fmt.Errorf("Failed to check slave pod status: %v", err)
			default:
				// 抛出错误
				return false, fmt.Errorf("The status return code of the position is incorrect: %v", statusCode)
			}
		}
		return true, nil
	}
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeoutSecond*time.Second, true, condition)
	return readySlavePods, skipSlavePods, resCode, err
}

func Owner(pod *v1.Pod) []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         "v1",
			Kind:               "Pod",
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			BlockOwnerDeletion: func(b bool) *bool { return &b }(true),
			Controller:         func(b bool) *bool { return &b }(true),
		},
	}
}

func (s *DeviceMounterImpl) GetSlavePods(devType string, ownerPod *v1.Pod, container *api.Container) ([]*v1.Pod, error) {
	selector := labels.SelectorFromSet(labels.Set{
		config.OwnerUidLabelKey:       string(ownerPod.UID),
		config.MountContainerLabelKey: container.Name,
	})
	pods, err := s.PodLister.Pods(ownerPod.Namespace).List(selector)
	if err != nil {
		return nil, err
	}
	var slavePods []*v1.Pod
	for i, pod := range pods {
		if pod.Annotations != nil && pod.Annotations[config.DeviceTypeAnnotationKey] == devType {
			slavePods = append(slavePods, pods[i])
		}
	}
	return slavePods, nil
}

func (s *DeviceMounterImpl) CreateSlavePodPDB(ctx context.Context, slavePod *v1.Pod) (*policyv1.PodDisruptionBudget, error) {
	pdb := policyv1.PodDisruptionBudget{}
	pdb.Name = slavePod.Name
	pdb.Namespace = slavePod.Namespace
	pdb.Labels = map[string]string{
		config.AppComponentLabelKey: "k8s-device-mounter",
		config.AppManagedByLabelKey: "k8s-device-mounter",
	}
	pdb.OwnerReferences = Owner(slavePod)
	// TODO 确保至少有1个Pod副本在任何中断期间都是可用的 (防止资源泄漏)
	minAvailable := intstr.FromInt32(int32(1))
	pdb.Spec.MinAvailable = &minAvailable
	pdb.Spec.Selector = &metav1.LabelSelector{MatchLabels: slavePod.Labels}
	return s.KubeClient.PolicyV1().PodDisruptionBudgets(slavePod.Namespace).
		Create(ctx, &pdb, metav1.CreateOptions{})
}

func (s *DeviceMounterImpl) MutationPodFunc(devType string, container *api.Container, ownerPod, mutaPod *v1.Pod) {
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
	mutaPod.Annotations[config.DeviceTypeAnnotationKey] = devType

	mutaPod.Spec.NodeSelector["kubernetes.io/hostname"] = s.NodeName
	mutaPod.Labels[config.OwnerUidLabelKey] = string(ownerPod.UID)
	mutaPod.Labels[config.CreatedByLabelKey] = uuid.New().String()
	mutaPod.Labels[config.MountContainerLabelKey] = container.Name
	mutaPod.Labels[config.AppComponentLabelKey] = "k8s-device-mounter"
	mutaPod.Labels[config.AppManagedByLabelKey] = "k8s-device-mounter"
	mutaPod.OwnerReferences = Owner(ownerPod)
	for i, _ := range mutaPod.Spec.Containers {
		mutaPod.Spec.Containers[i].Image = config.DeviceSlaveContainerImageTag
		mutaPod.Spec.Containers[i].ImagePullPolicy = config.DeviceSlaveImagePullPolicy
	}
	mutaPod.Spec.TerminationGracePeriodSeconds = pointer.Int64(0)
}

func (s *DeviceMounterImpl) GetCGroupPath(pod *v1.Pod, container *api.Container) (string, error) {
	oldversion := false
loop:
	// 获取容器cgroup路径
	cgroupPath, err := util.GetK8sPodCGroupPath(pod, container, oldversion)
	if err != nil {
		return "", err
	}
	if s.IsCGroupV2 {
		cgroupPath = util.GetGroupPathV2(cgroupPath)
	} else {
		cgroupPath = util.GetDeviceGroupPathV1(cgroupPath)
	}
	if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
		if !oldversion {
			klog.Warning("cgroup path ", cgroupPath, " not found, try use old version")
			oldversion = true
			goto loop
		}
		return "", fmt.Errorf("The container cgroup path does not exist: %s", cgroupPath)
	}
	return cgroupPath, nil
}

func (s *DeviceMounterImpl) DeviceRuleSetFunc(cgroupPath string, r *configs.Resources) (closed func() error, rollback func() error, err error) {
	if len(r.Devices) == 0 {
		klog.V(3).Infoln("No device information to be mounted, skipping device permission settings")
		goto skipDeviceSet
	}
	if s.IsCGroupV2 {
		klog.V(3).Infoln("Use cgroupv2 ebpf device controller")
		closed, rollback, err = util.SetDeviceRulesV2(cgroupPath, r)
		return
	} else {
		closed = util.NilCloser
		rollback, err = util.SetDeviceRulesV1(cgroupPath, r)
		return
	}
skipDeviceSet:
	closed = util.NilCloser
	rollback = util.NilCloser
	return
}

func (s *DeviceMounterImpl) CreateDeviceFiles(cfg *util.Config, devInfos []api.DeviceInfo) (func() error, error) {
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

func (s *DeviceMounterImpl) DeleteDeviceFiles(cfg *util.Config, devInfos []api.DeviceInfo) (func() error, error) {
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
