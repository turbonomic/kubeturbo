package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ReScheduler struct {
	kubeClient *kclient.Clientset
	lockMap    *util.ExpirationMap
}

func NewReScheduler(client *kclient.Clientset, lmap *util.ExpirationMap) *ReScheduler {
	return &ReScheduler{
		kubeClient: client,
		lockMap:    lmap,
	}
}

//Note: the error info will be shown in UI
func (r *ReScheduler) Execute(actionItem *proto.ActionItemDTO) error {
	if actionItem == nil {
		glog.Errorf("container resize ActionItem passed in is nil")
		return fmt.Errorf("Invalid Action Info")
	}

	//1. get target Pod and new hosting Node
	pod, node, err := r.getPodNode(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute resizeAction: failed to get target pod or new hosting node: %v", err)
		return fmt.Errorf("Failed")
	}

	//2. move pod to the node
	npod, err := r.reSchedule(pod, node)
	if err != nil {
		glog.Errorf("Failed to execute resizeAction: %v\n %++v", err, actionItem)
		return err
	}

	//3. check Pod
	fullName := util.BuildIdentifier(npod.Namespace, npod.Name)
	nodeName := npod.Spec.NodeName
	glog.V(2).Infof("Begin to check resizeAction for pod[%v]", fullName)
	if err = r.checkPod(npod, nodeName); err != nil {
		glog.Errorf("Checking resizeAction failed: pod[%v] failed: %v", fullName, err)
		return fmt.Errorf("Check Failed")
	}
	glog.V(2).Infof("Checking resizeAction succeeded: pod[%v] is on node[%v].", fullName, nodeName)

	return nil
}

func (r *ReScheduler) checkActionItem(action *proto.ActionItemDTO) error {
	//1. check target
	targetSE := action.GetTargetSE()
	if targetSE == nil {
		err := fmt.Errorf("move target is empty.")
		glog.Error(err.Error())
		return err
	}

	if targetSE.GetEntityType() != proto.EntityDTO_CONTAINER_POD {
		err := fmt.Errorf("Unexpected target type: %v Vs. %v.",
			targetSE.GetEntityType().String(), proto.EntityDTO_CONTAINER_POD.String())
		glog.Errorf(err.Error())
		return err
	}

	//2. check new hosting node
	destSE := action.GetNewSE()
	if destSE == nil {
		err := fmt.Errorf("new hosting node is empty: %++v", action)
		glog.Error(err.Error())
		return err
	}

	nodeType := destSE.GetEntityType()
	if nodeType != proto.EntityDTO_VIRTUAL_MACHINE && nodeType != proto.EntityDTO_PHYSICAL_MACHINE {
		err := fmt.Errorf("hosting node type[%v] is not a virtual machine, nor a phsical machine.", nodeType.String())
		glog.Error(err.Error())
		return err
	}

	return nil
}

// get k8s.node of the new hosting node
func (r *ReScheduler) getNode(action *proto.ActionItemDTO) (*api.Node, error) {
	hostSE := action.GetNewSE()
	if hostSE == nil {
		err := fmt.Errorf("new host pod entity is empty")
		glog.Error(err.Error())
		return nil, err
	}

	var err error = nil
	var node *api.Node = nil

	//0. check entity type
	etype := hostSE.GetEntityType()
	if etype != proto.EntityDTO_VIRTUAL_MACHINE && etype != proto.EntityDTO_PHYSICAL_MACHINE {
		err = fmt.Errorf("The target entity(%v) for move destiantion is neither a VM nor a PM.", etype)
		glog.Error(err.Error())
		return nil, err
	}

	//1. get node from properties
	node, err = util.GetNodeFromProperties(r.kubeClient, hostSE.GetEntityProperties())
	if err == nil {
		glog.V(2).Infof("Get node(%v) from properties.", node.Name)
		return node, nil
	}

	//2. get node by displayName
	node, err = util.GetNodebyName(r.kubeClient, hostSE.GetDisplayName())
	if err == nil {
		glog.V(2).Infof("Get node(%v) by displayName.", node.Name)
		return node, nil
	}

	//3. get node by UUID
	node, err = util.GetNodebyUUID(r.kubeClient, hostSE.GetId())
	if err == nil {
		glog.V(2).Infof("Get node(%v) by UUID(%v).", node.Name, hostSE.GetId())
		return node, nil
	}

	//4. get node by IP
	vmIPs := getVMIps(hostSE)
	if len(vmIPs) > 0 {
		node, err = util.GetNodebyIP(r.kubeClient, vmIPs)
		if err == nil {
			glog.V(2).Infof("Get node(%v) by IP.", hostSE.GetDisplayName())
			return node, nil
		} else {
			glog.Errorf("Failed to get node by IP(%+v): %v", vmIPs, err)
		}
	} else {
		glog.Warningf("VMIPs are empty: %++v", hostSE)
	}

	glog.Errorf("Failed to get node(%v) %++v", hostSE.GetDisplayName(), hostSE)
	err = fmt.Errorf("Failed to get node(%v)", hostSE.GetDisplayName())
	return nil, err
}

func (r *ReScheduler) getTargetPod(podEntity *proto.EntityDTO) (*api.Pod, error) {
	//1. try to get pod from properties
	// TODO, as there is issue in server, find pod based on entity properties is not supported right now.
	// Once the issue in server gets resolved, we can get rid of finding pod by name or uuid.
	etype := podEntity.GetEntityType()
	properties := podEntity.GetEntityProperties()
	pod, err := util.GetPodFromProperties(r.kubeClient, etype, properties)
	if err != nil {
		glog.Warningf("Failed to get pod from podEntity properites: %++v", podEntity)
	} else {
		return pod, nil
	}

	//2. try to get pod from UUID
	return util.GetPodFromDisplayNameOrUUID(r.kubeClient, podEntity.GetDisplayName(), podEntity.GetId())
}

// get kubernetes pod, and the new hosting kubernetes node
func (r *ReScheduler) getPodNode(action *proto.ActionItemDTO) (*api.Pod, *api.Node, error) {
	//1. check
	glog.V(4).Infof("MoveActionItem: %++v", action)
	if err := r.checkActionItem(action); err != nil {
		err = fmt.Errorf("Move Action aborted: check action item failed: %v", err)
		glog.Errorf(err.Error())
		return nil, nil, err
	}

	//2. get pod from k8s
	target := action.GetTargetSE()
	pod, err := r.getTargetPod(target)
	if err != nil {
		err = fmt.Errorf("Move Action aborted: failed to find pod(%v) in k8s: %v", target.GetDisplayName(), err)
		glog.Errorf(err.Error())
		return nil, nil, err
	}

	//3. find the new hosting node for the pod.
	node, err := r.getNode(action)
	if err != nil {
		err = fmt.Errorf("Move action aborted: failed to get new hosting node: %v", err)
		glog.Error(err.Error())
		return nil, nil, err
	}

	return pod, node, nil
}

// Check whether the action should be executed.
// TODO: find a reliable way to check node's status; current checking has no actual effect.
func (r *ReScheduler) preActionCheck(pod *api.Pod, node *api.Node) error {
	fullName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// Check if the pod privilege is supported
	if !util.SupportPrivilegePod(pod) {
		err := fmt.Errorf("The pod %s has privilege requirement unsupported", fullName)
		glog.Error(err)
		return err
	}

	//1. If Pod is terminated, then no need to move it.
	// if pod.Status.Phase != api.PodRunning {
	if pod.Status.Phase == api.PodSucceeded {
		glog.Errorf("Move action should be aborted: original pod termiated:%v phase:%v", fullName, pod.Status.Phase)
	}

	//2. if Node is out of condition
	conditions := node.Status.Conditions
	if conditions == nil || len(conditions) < 1 {
		glog.Warningf("Move action: pod[%v]'s new host(%v) condition is unknown", fullName, node.Name)
		return nil
	}

	for _, cond := range conditions {
		if cond.Status != api.ConditionTrue {
			glog.Warningf("Move action: pod[%v]'s new host(%v) in bad condition: %v", fullName, node.Name, cond.Type)
		}
	}

	return nil
}

func (r *ReScheduler) reSchedule(pod *api.Pod, node *api.Node) (*api.Pod, error) {
	//1. do some check
	if err := r.preActionCheck(pod, node); err != nil {
		glog.Errorf("Move action aborted: %v", err)
		return nil, fmt.Errorf("Failed")
	}

	nodeName := node.Name
	fullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	// if the pod is already on the target node, then simply return success.
	if pod.Spec.NodeName == nodeName {
		glog.V(2).Infof("Move action aborted: pod[%v] is already on host[%v].", fullName, nodeName)
		return nil, fmt.Errorf("Aborted")
	}

	//2. move
	parentKind, parentName, err := util.GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("Move action aborted: cannot get pod-%v parent info: %v", fullName, err)
		return nil, fmt.Errorf("Failed")
	}

	if !util.SupportedParent(parentKind) {
		glog.Errorf("Move action aborted: parent kind(%v) is not supported.", parentKind)
		return nil, fmt.Errorf("Unsupported")
	}

	var npod *api.Pod
	if parentKind == "" {
		npod, err = r.moveBarePod(pod, nodeName)
	} else {
		npod, err = r.moveControllerPod(pod, parentKind, parentName, nodeName)
	}

	if err != nil {
		glog.Errorf("Move Pod(%v) action failed: %v", fullName, err)
		return nil, fmt.Errorf("Failed")
	}
	return npod, nil
}

//TODO: make it as a function for LockHelper
func (r *ReScheduler) getLock(key string) (*util.LockHelper, error) {
	//1. set up lock helper
	helper, err := util.NewLockHelper(key, r.lockMap)
	if err != nil {
		glog.Errorf("Failed to get a lockHelper: %v", err)
		return nil, err
	}

	// 2. wait to get a lock of current Pod
	timeout := defaultWaitLockTimeOut
	interval := defaultWaitLockSleep
	err = helper.Trylock(timeout, interval)
	if err != nil {
		glog.Errorf("Failed to acquire lock with key(%v): %v", key, err)
		return nil, err
	}
	return helper, nil
}

// move the pods controlled by ReplicationController/ReplicaSet
func (r *ReScheduler) moveControllerPod(pod *api.Pod, parentKind, parentName, nodeName string) (*api.Pod, error) {
	controllerKey := fmt.Sprintf("%s-%v/%v", parentKind, pod.Namespace, parentName)
	helper, err := r.getLock(controllerKey)
	if err != nil {
		glog.Errorf("Move controller(%v) Pod(%v) failed: failed to get lock %v", controllerKey, pod.Name, err)
		return nil, err
	}

	defer helper.ReleaseLock()

	glog.V(3).Infof("Get lock for pod[%s] parent[%v-%s]", pod.Name, parentKind, parentName)
	helper.KeepRenewLock()

	return movePod(r.kubeClient, pod, nodeName, defaultRetryMore)
}

// as there may be concurrent actions on the same bare pod:
//   for example, one action is to move Pod, and the other is to Resize Pod.container;
// thus, concurrent control should also be applied to bare pods.
func (r *ReScheduler) moveBarePod(pod *api.Pod, nodeName string) (*api.Pod, error) {
	podkey := util.BuildIdentifier(pod.Namespace, pod.Name)
	// 1. setup lockHelper
	helper, err := r.getLock(podkey)
	if err != nil {
		glog.Errorf("Move bare Pod(%v) failed: failed to get lock %v", podkey, err)
		return nil, err
	}
	defer helper.ReleaseLock()

	// 2. move the Pod
	helper.KeepRenewLock()
	return movePod(r.kubeClient, pod, nodeName, defaultRetryMore)
}

func (r *ReScheduler) checkPod(pod *api.Pod, nodeName string) error {
	retryNum := defaultRetryLess
	interval := defaultPodCheckSleep
	timeout := time.Duration(retryNum+1) * interval
	err := goutil.RetrySimple(retryNum, timeout, interval, func() (bool, error) {
		return doCheckPodNode(r.kubeClient, pod.Namespace, pod.Name, nodeName)
	})

	if err != nil {
		return err
	}

	return nil
}

func getVMIps(entity *proto.EntityDTO) []string {
	result := []string{}

	if entity.GetEntityType() != proto.EntityDTO_VIRTUAL_MACHINE {
		glog.Errorf("hosting node is a not virtual machine: %++v", entity.GetEntityType())
		return result
	}

	vmData := entity.GetVirtualMachineData()
	if vmData == nil {
		err := fmt.Errorf("Missing virtualMachineData[%v] in targetSE.", entity.GetDisplayName())
		glog.Error(err.Error())
		return result
	}

	if len(vmData.GetIpAddress()) < 1 {
		glog.Warningf("machine IPs are empty: %++v", vmData)
	}

	return vmData.GetIpAddress()
}
