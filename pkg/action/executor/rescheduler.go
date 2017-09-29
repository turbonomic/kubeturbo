package executor

import (
	"errors"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	"time"

	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	// Set the grace period to 0 for deleting the pod immediately.
	podDeletionGracePeriodDefault int64 = 0

	//TODO: set podDeletionGracePeriodMax > 0
	// currently, if grace > 0, there will be some retries and could cause timeout easily
	podDeletionGracePeriodMax int64 = 0

	DefaultNoneExistSchedulerName = "turbo-none-exist-scheduler"
	kindReplicationController     = "ReplicationController"
	kindReplicaSet                = "ReplicaSet"

	HigherK8sVersion = "1.6.0"

	defaultRetryLess int = 3
	defaultRetryMore int = 6

	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10

	defaultPodCreateSleep       = time.Second * 30
	defaultUpdateSchedulerSleep = time.Second * 20
	defaultCheckSchedulerSleep  = time.Second * 5
	defaultUpdateReplicaSleep  = time.Second * 20
	defaultMoreGrace            = time.Second * 20
)

type ReScheduler struct {
	kubeClient        *kclient.Clientset
	k8sVersion        string
	noneSchedulerName string

	//a map for concurrent control of Actions
	lockMap *util.ExpirationMap
}

func NewReScheduler(client *kclient.Clientset, k8sver, noschedulerName string, lmap *util.ExpirationMap) *ReScheduler {
	return &ReScheduler{
		kubeClient:        client,
		k8sVersion:        k8sver,
		noneSchedulerName: noschedulerName,
		lockMap:           lmap,
	}
}

func (r *ReScheduler) Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error) {
	if actionItem == nil {
		return nil, errors.New("ActionItem passed in is nil")
	}
	action, err := r.buildPendingReScheduleTurboAction(actionItem)
	if err != nil {
		return nil, err
	}
	return r.reSchedule(action)
}

func (r *ReScheduler) buildPendingReScheduleTurboAction(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction,
	error) {

	glog.V(4).Infof("MoveActionItem: %++v", actionItem)
	// Find out the pod to be re-scheduled.
	targetPod := actionItem.GetTargetSE()
	if targetPod == nil {
		return nil, errors.New("Target pod in actionItem is nil")
	}

	// TODO, as there is issue in server, find pod based on entity properties is not supported right now. Once the issue in server gets resolved, we should use the following code to find the pod.
	//podProperties := targetPod.GetEntityProperties()
	//foundPod, err:= util.GetPodFromProperties(h.kubeClient, targetEntityType, podProperties)

	// TODO the following is a temporary fix.
	originalPod, err := util.GetPodFromUUID(r.kubeClient, targetPod.GetId())
	if err != nil {
		glog.Errorf("Cannot not find pod %s in the cluster: %s", targetPod.GetDisplayName(), err)
		return nil, fmt.Errorf("Try to move pod %s, but could not find it in the cluster.",
			targetPod.GetDisplayName())
	}

	// Find out where to re-schedule the pod.
	newSEType := actionItem.GetNewSE().GetEntityType()
	if newSEType != proto.EntityDTO_VIRTUAL_MACHINE && newSEType != proto.EntityDTO_PHYSICAL_MACHINE {
		return nil, errors.New("The target service entity for move destiantion is neither a VM nor a PM.")
	}
	targetNode := actionItem.GetNewSE()

	var machineIPs []string
	switch newSEType {
	case proto.EntityDTO_VIRTUAL_MACHINE:
		// K8s uses Ip address as the Identifier. The VM name passed by actionItem is the display name
		// that discovered by hypervisor. So here we must get the ip address from virtualMachineData in
		// targetNode entityDTO.
		vmData := targetNode.GetVirtualMachineData()
		if vmData == nil {
			return nil, errors.New("Missing VirtualMachineData in ActionItemDTO from server")
		}
		machineIPs = vmData.GetIpAddress()
		break
	case proto.EntityDTO_PHYSICAL_MACHINE:
		// TODO
		// machineIPS = <valid physical machine IP>
		break
	}
	if machineIPs == nil || len(machineIPs) == 0 {
		return nil, errors.New("Miss IP addresses in ActionItemDTO.")
	}
	glog.V(3).Infof("The IPs of targetNode is %v", machineIPs)

	// Get the actual node name from Kubernetes cluster based on IP address.
	nodeIdentifier, err := util.GetNodeNameFromIP(r.kubeClient, machineIPs)
	if err != nil {
		return nil, err
	}

	// Build action content.
	var parentObjRef *turboaction.ParentObjectRef = nil
	// parent info is not necessary for current binding-on-creation method

	//// TODO, Ignore error?
	//parentRefObject, err := probe.FindParentReferenceObject(originalPod)
	//if err != nil {
	//	return nil, err
	//}
	//if parentRefObject != nil {
	//	parentObjRef = &turboaction.ParentObjectRef{
	//		ParentObjectUID:       string(parentRefObject.UID),
	//		ParentObjectNamespace: parentRefObject.Namespace,
	//		ParentObjectName:      parentRefObject.Name,
	//		ParentObjectType:      parentRefObject.Kind,
	//	}
	//}
	targetObj := &turboaction.TargetObject{
		TargetObjectUID:       string(originalPod.UID),
		TargetObjectNamespace: originalPod.Namespace,
		TargetObjectName:      originalPod.Name,
		TargetObjectType:      turboaction.TypePod,
	}
	moveSpec := turboaction.MoveSpec{
		Source:      originalPod.Spec.NodeName,
		Destination: nodeIdentifier,
	}
	content := turboaction.NewTurboActionContentBuilder(turboaction.ActionMove, targetObj).
		ActionSpec(moveSpec).
		ParentObjectRef(parentObjRef).
		Build()

	// Build TurboAction.
	action := turboaction.NewTurboActionBuilder(originalPod.Namespace, *actionItem.Uuid).
		Content(content).
		Create()
	return &action, nil
}

// Check whether the action should be executed.
func (r *ReScheduler) preActionCheck(podName, namespace, nodeName string) (*api.Pod, error) {
	fullName := fmt.Sprintf("%s/%s", namespace, podName)
	podClient := r.kubeClient.CoreV1().Pods(namespace)
	if podClient == nil {
		return nil, fmt.Errorf("re-schedule failed: fail to get pod client in namespace [%v]", namespace)
	}

	pod, err := podClient.Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("re-schedule failed: get original pod:%v\n%v", fullName, err)
	}

	// If Pod is terminated, then no need to move it.
	// if pod.Status.Phase != api.PodRunning {
	if pod.Status.Phase == api.PodSucceeded {
		return nil, fmt.Errorf("re-schedule failed: original pod termiated:%v phase:%v", fullName, pod.Status.Phase)
	}

	return pod, nil
}

// move the pods controlled by ReplicationController/ReplicaSet
func (r *ReScheduler) moveControllerPod(pod *api.Pod, parentKind, parentName, nodeName string) (*api.Pod, error) {
	highver := true
	if goutil.CompareVersion(r.k8sVersion, HigherK8sVersion) < 0 {
		highver = false
	}

	//1. set up
	noexist := r.noneSchedulerName
	helper, err := NewSchedulerHelper(r.kubeClient, pod.Namespace, pod.Name, parentKind, parentName, noexist, highver)
	if err != nil {
		return nil, err
	}
	if err := helper.SetMap(r.lockMap); err != nil {
		return nil, err
	}

	//2. wait to get a lock
	interval := defaultWaitLockSleep
	err = goutil.RetryDuring(1000, defaultWaitLockTimeOut, interval, func() error {
		if !helper.Acquirelock() {
			return fmt.Errorf("TryLater")
		}
		return nil
	})
	if err != nil {
		glog.V(3).Infof("Move pod[%s] failed: Failed to acuire lock parent[%s]", pod.Name, parentName)
		return nil, err
	}
	defer func() {
		helper.CleanUp()
		util.CleanPendingPod(r.kubeClient, pod.Namespace, noexist, parentKind, parentName, highver)
	}()
	glog.V(3).Infof("Get lock for pod[%s] parent[%s]", pod.Name, parentName)
	helper.KeepRenewLock()

	//3. invalidate the scheduler of the parentController
	preScheduler, err := helper.UpdateScheduler(noexist, defaultRetryLess)
	if err != nil {
		glog.Errorf("Move pod[%s] failed: failed to invalidate schedulerName parent[%s]", pod.Name, parentName)
		return nil, fmt.Errorf("TryLater")
	}

	//4. set the original scheduler for restore
	helper.SetScheduler(preScheduler)

	return movePod(r.kubeClient, pod, nodeName, defaultRetryLess)
}

func (r *ReScheduler) reSchedule(action *turboaction.TurboAction) (*turboaction.TurboAction, error) {
	actionContent := action.Content
	moveSpec, ok := actionContent.ActionSpec.(turboaction.MoveSpec)
	if !ok || moveSpec.Destination == "" {
		return nil, fmt.Errorf("re-scheduler failed: destination is nil after pre-precess.")
	}

	//1. get original Pod, and do some check
	podName := actionContent.TargetObject.TargetObjectName
	namespace := actionContent.TargetObject.TargetObjectNamespace
	nodeName := moveSpec.Destination

	pod, err := r.preActionCheck(podName, namespace, nodeName)
	if err != nil {
		return nil, err
	}

	// if the pod is already on the target node, then simply return success.
	if pod.Spec.NodeName == nodeName {
		action.Status = turboaction.Success
		action.LastTimestamp = time.Now()
		return action, nil
	}

	//2. move
	fullName := util.BuildIdentifier(namespace, podName)
	parentKind, parentName, err := util.ParseParentInfo(pod)
	if err != nil {
		return nil, fmt.Errorf("move-abort: cannot get pod-%v parent info: %v", fullName, err)
	}

	var npod *api.Pod
	if parentKind == "" {
		npod, err = movePod(r.kubeClient, pod, nodeName, defaultRetryLess)
	} else {
		npod, err = r.moveControllerPod(pod, parentKind, parentName, nodeName)
	}
	if err != nil {
		glog.Errorf("move pod [%s] failed: %v", fullName, err)
		return nil, fmt.Errorf("move failed: %s", fullName)
	}

	//3. update moveAction
	moveSpec.NewObjectName = npod.Name
	moveSpec.NewObjectNamespace = npod.Namespace
	action.Content.ActionSpec = moveSpec
	action.Status = turboaction.Executed
	action.LastTimestamp = time.Now()

	return action, nil
}
