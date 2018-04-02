package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	api "k8s.io/client-go/pkg/api/v1"

	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ReScheduler struct {
	TurboK8sActionExecutor
}

func NewReScheduler(ae TurboK8sActionExecutor) *ReScheduler {
	return &ReScheduler{
		TurboK8sActionExecutor: ae,
	}
}

//Note: the error info will be shown in UI
func (r *ReScheduler) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItem
	pod := input.Pod

	//1. get target Pod and new hosting Node
	node, err := r.getPodNode(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute pod move: failed to get target pod or new hosting node: %v", err)
		return &TurboActionExecutorOutput{}, fmt.Errorf("Failed")
	}

	//2. move pod to the node
	npod, err := r.reSchedule(pod, node)
	if err != nil {
		glog.Errorf("Failed to execute pod move: %v\n %++v", err, actionItem)
		return &TurboActionExecutorOutput{}, err
	}

	//3. check Pod
	fullName := util.BuildIdentifier(npod.Namespace, npod.Name)
	nodeName := npod.Spec.NodeName
	glog.V(2).Infof("Begin to check pod move for pod[%v]", fullName)
	if err = r.checkPod(npod, nodeName); err != nil {
		glog.Errorf("Checking pod move failed: pod[%v] failed: %v", fullName, err)
		return &TurboActionExecutorOutput{}, fmt.Errorf("Check Failed")
	}
	glog.V(2).Infof("Checking pod move succeeded: pod[%v] is on node[%v].", fullName, nodeName)

	return &TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    pod,
		NewPod:    npod,
	}, nil
}

func (r *ReScheduler) checkActionItem(action *proto.ActionItemDTO) error {
	// check new hosting node
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

// get kubernetes pod, and the new hosting kubernetes node
func (r *ReScheduler) getPodNode(action *proto.ActionItemDTO) (*api.Node, error) {
	//1. check
	glog.V(4).Infof("MoveActionItem: %++v", action)
	if err := r.checkActionItem(action); err != nil {
		err = fmt.Errorf("Move Action aborted: check action item failed: %v", err)
		glog.Errorf(err.Error())
		return nil, err
	}
	//2. find the new hosting node for the pod.
	node, err := r.getNode(action)
	if err != nil {
		err = fmt.Errorf("Move action aborted: failed to get new hosting node: %v", err)
		glog.Error(err.Error())
		return nil, err
	}

	return node, nil
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
	parentKind, parentName, err := podutil.GetPodParentInfo(pod)
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

// move the pods controlled by ReplicationController/ReplicaSet
func (r *ReScheduler) moveControllerPod(pod *api.Pod, parentKind, parentName, nodeName string) (*api.Pod, error) {
	npod, err := movePod(r.kubeClient, pod, nodeName, defaultRetryMore)
	if err != nil {
		glog.Errorf("Move contorller pod(%s) failed: %v", pod.Name, err)
	}

	return npod, err
}

// as there may be concurrent actions on the same bare pod:
//   for example, one action is to move Pod, and the other is to Resize Pod.container;
// thus, concurrent control should also be applied to bare pods.
func (r *ReScheduler) moveBarePod(pod *api.Pod, nodeName string) (*api.Pod, error) {
	npod, err := movePod(r.kubeClient, pod, nodeName, defaultRetryMore)
	if err != nil {
		glog.Errorf("Move contorller pod(%s) failed: %v", pod.Name, err)
	}

	return npod, err
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
