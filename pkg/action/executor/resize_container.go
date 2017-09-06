package executor

import (
	kclient "k8s.io/client-go/kubernetes"
	k8sapi "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	idutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"fmt"
	"github.com/golang/glog"
	"time"
)

type ContainerResizer struct {
	kubeClient        *kclient.Clientset
	kubeletClient     *kubelet.KubeletClient
	broker            turbostore.Broker
	k8sVersion        string
	noneSchedulerName string

	//a map for concurrent control of Actions
	lockMap *util.ExpirationMap
}

func NewContainerResizer(client *kclient.Clientset, kubeletClient *kubelet.KubeletClient, broker turbostore.Broker, k8sver, noschedulerName string, lmap *util.ExpirationMap) *ContainerResizer {
	return &ContainerResizer{
		kubeClient:        client,
		kubeletClient:     kubeletClient,
		broker:            broker,
		k8sVersion:        k8sver,
		noneSchedulerName: noschedulerName,
		lockMap:           lmap,
	}
}

// get node cpu frequency, in KHz;
func (r *ContainerResizer) getNodeCPUFrequency(host string) (uint64, error) {
	return r.kubeletClient.GetMachineCpuFrequency(host)
}

func (r *ContainerResizer) setCPUCapacity(cpuMhz float64, host string, rlist k8sapi.ResourceList) error {
	cpuFrequency, err := r.getNodeCPUFrequency(host)
	if err != nil {
		glog.Errorf("failed to get node[%s] cpu frequency: %v", host, err)
		return err
	}

	cpuQuantity, err := genCPUQuantity(cpuMhz, cpuFrequency)
	if err != nil {
		glog.Errorf("failed to generate CPU quantity: %v", err)
		return err
	}

	rlist[k8sapi.ResourceCPU] = cpuQuantity
	return nil
}

// get commodity type and new capacity, and convert it into a k8s.Quantity.
func (r *ContainerResizer) buildNewCapacity(pod *k8sapi.Pod, actionItem *proto.ActionItemDTO) (k8sapi.ResourceList, error) {
	result := make(k8sapi.ResourceList)

	comm := actionItem.GetNewComm()
	ctype := comm.GetCommodityType()
	amount := comm.GetCapacity()

	//TODO: currently only support resizeCapacity; resizeReservation may be supported later
	if amount < 1 {
		msg := ""
		if comm.GetReservation() > 0 {
			msg = fmt.Sprintf("resizeReservation() is not supported yet.")
		} else {
			msg = fmt.Sprintf("new capacity should be bigger than zero (current=%.4f)", amount)
		}

		glog.Error(msg)
		return result, fmt.Errorf(msg)
	}

	switch ctype {
	case proto.CommodityDTO_VCPU:
		host := pod.Spec.NodeName
		err := r.setCPUCapacity(amount, host, result)
		if err != nil {
			glog.Errorf("failed to build cpu.Capacity: %v", err)
			return result, err
		}
	case proto.CommodityDTO_VMEM:
		memory, err := genMemoryQuantity(amount)
		if err != nil {
			glog.Errorf("failed to build mem.Capacity: %v", err)
			return result, err
		}
		result[k8sapi.ResourceMemory] = memory
	default:
		err := fmt.Errorf("Unsupport Commodity type[%v]", ctype)
		glog.Error(err)
		return result, err
	}

	return result, nil
}

func (r *ContainerResizer) buildResizeAction(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, *k8sapi.Pod, error) {

	//1. get hosting Pod and containerIndex
	entity := actionItem.GetTargetSE()
	containerId := entity.GetId()

	podId, containerIndex, err := idutil.ParseContainerId(containerId)
	if err != nil {
		glog.Errorf("failed to parse podId to build resizeAction: %v", err)
		return nil, nil, err
	}

	podEntity := actionItem.GetHostedBySE()
	podId2 := podEntity.GetId()
	if podId2 != podId {
		err = fmt.Errorf("hosting pod(%s) Id mismatch [%v Vs. %v]", podEntity.GetDisplayName(), podId, podId2)
		glog.Error(err)
		return nil, nil, err
	}

	pod, err := util.GetPodFromUUID(r.kubeClient, podId)
	if err != nil {
		glog.Errorf("failed to get hosting Pod to build resizeAction: %v", err)
		return nil, nil, err
	}

	//2. build the new resource Capacity
	newCapacity, err := r.buildNewCapacity(pod, actionItem)
	if err != nil {
		glog.Errorf("failed to build NewCapacity to build resizeAction: %v", err)
		return nil, nil, err
	}

	//3. build the turboAction object
	targetObj := &turboaction.TargetObject{
		TargetObjectUID:       string(pod.UID),
		TargetObjectNamespace: pod.Namespace,
		TargetObjectName:      pod.Name,
		TargetObjectType:      turboaction.TypePod,
	}

	resizeSpec := turboaction.ContainerResizeSpec{
		Index:       containerIndex,
		NewCapacity: newCapacity,
	}

	var parentObjRef *turboaction.ParentObjectRef = nil

	content := turboaction.NewTurboActionContentBuilder(turboaction.ActionContainerResize, targetObj).
		ActionSpec(resizeSpec).
		ParentObjectRef(parentObjRef).Build()

	action := turboaction.NewTurboActionBuilder(pod.Namespace, *actionItem.Uuid).
		Content(content).
		Create()

	return &action, pod, nil
}

func (r *ContainerResizer) Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error) {
	if actionItem == nil {
		glog.Errorf("potential bug: actionItem is null.")
		return nil, fmt.Errorf("ActionItem is null.")
	}

	//1. build turboAction
	action, pod, err := r.buildResizeAction(actionItem)
	if err != nil {
		glog.Errorf("failed to execute container resize: %v", err)
		return nil, err
	}

	//2. execute the turboAction
	err = r.executeAction(action, pod)
	if err != nil {
		glog.Errorf("failed to execute Action: %v", err)
		return nil, err
	}

	action.LastTimestamp = time.Now()
	if action.Status == turboaction.Success {
		return action, nil
	}
	action.Status = turboaction.Executed

	return action, nil
}

func (r *ContainerResizer) executeAction(action *turboaction.TurboAction, pod *k8sapi.Pod) error {
	//1. check
	content := action.Content
	resizeSpec, ok := content.ActionSpec.(turboaction.ContainerResizeSpec)
	if !ok {
		return fmt.Errorf("resizeContainer failed")
	}
	if len(resizeSpec.NewCapacity) < 1 {
		action.Status = turboaction.Success
		return nil
	}

	//2. get parent controller
	fullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	parentKind, parentName, err := util.ParseParentInfo(pod)
	if err != nil {
		glog.Errorf("failed to get pod[%s] parent info: %v", fullName, err)
		return err
	}

	if parentKind == "" {
		err = resizeContainer(r.kubeClient, pod, resizeSpec.Index, resizeSpec.NewCapacity, defaultRetryMore)
	} else {
		err = r.resizeControllerContainer(pod, parentKind, parentName, resizeSpec.Index, resizeSpec.NewCapacity)
	}

	if err != nil {
		glog.Errorf("resize Pod[%s]-%d container failed: %v", fullName, resizeSpec.Index)
	}

	return nil
}

func (r *ContainerResizer) resizeControllerContainer(pod *k8sapi.Pod, parentKind, parentName string, index int, capacity k8sapi.ResourceList) error {
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, index)
	glog.V(2).Infof("begin to resizeContainer[%s] parent=%s/%s.", id, parentKind, parentName)

	//1. set up
	highver := true
	if goutil.CompareVersion(r.k8sVersion, HigherK8sVersion) < 0 {
		highver = false
	}
	noexist := r.noneSchedulerName
	helper, err := NewMoveHelper(r.kubeClient, pod.Namespace, pod.Name, parentKind, parentName, noexist, highver)
	if err != nil {
		glog.Errorf("resizeContainer failed[%s]: failed to create helper: %v", id, err)
		return err
	}
	if err := helper.SetMap(r.lockMap); err != nil {
		return err
	}

	//2. wait to get a lock of the parent object
	timeout := defaultWaitLockTimeOut
	interval := defaultWaitLockSleep
	err = goutil.RetryDuring(1000, timeout, interval, func() error {
		if !helper.Acquirelock() {
			return fmt.Errorf("TryLater")
		}
		return nil
	})
	if err != nil {
		glog.Errorf("resizeContainer failed[%s]: failed to acquire lock of parent[%s]", id, parentName)
		return err
	}
	glog.V(3).Infof("resizeContainer [%s]: got lock for parent[%s]", id, parentName)
	helper.KeepRenewLock()

	//3. defer function for cleanUp
	defer func() {
		helper.CleanUp()
		util.CleanPendingPod(r.kubeClient, pod.Namespace, noexist, parentKind, parentName, highver)
	}()

	//4. disable the scheduler of  the parentController
	preScheduler, err := helper.UpdateScheduler(noexist, defaultRetryLess)
	if err != nil {
		glog.Errorf("resizeContainer failed[%s]: failed to disable parentController-[%s]'s scheduler.", id, parentName)
		return fmt.Errorf("TryLater")
	}

	//5.resize Container and restore parent's scheduler
	helper.SetScheduler(preScheduler)
	err = resizeContainer(r.kubeClient, pod, index, capacity, defaultRetryLess)
	if err != nil {
		glog.Errorf("resizeContainer failed[%s]: %v", id, err)
		return fmt.Errorf("TryLater")
	}

	return nil
}
