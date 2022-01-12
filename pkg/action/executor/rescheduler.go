package executor

import (
	"fmt"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	api "k8s.io/api/core/v1"

	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ReScheduler struct {
	TurboK8sActionExecutor
	sccAllowedSet           map[string]struct{}
	failVolumePodMoves      bool
	updateQuotaToAllowMoves bool
	lockMap                 *util.ExpirationMap
	failureThreshold        int
}

func NewReScheduler(ae TurboK8sActionExecutor, sccAllowedSet map[string]struct{},
	failVolumePodMoves, updateQuotaToAllowMoves bool, lockMap *util.ExpirationMap, failureThreshold int) *ReScheduler {
	return &ReScheduler{
		TurboK8sActionExecutor:  ae,
		sccAllowedSet:           sccAllowedSet,
		failVolumePodMoves:      failVolumePodMoves,
		updateQuotaToAllowMoves: updateQuotaToAllowMoves,
		lockMap:                 lockMap,
		failureThreshold:        failureThreshold,
	}
}

// Execute executes the move action. The error message will be shown in UI.
func (r *ReScheduler) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItems[0]
	pod := input.Pod

	//1. get target Pod and new hosting Node
	node, err := r.getPodNode(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute pod move: %v.", err)
		return &TurboActionExecutorOutput{}, err
	}

	//2. move pod to the node and check move status
	npod, err := r.reSchedule(pod, node)
	if err != nil {
		glog.Errorf("Failed to execute pod move: %v.", err)
		return &TurboActionExecutorOutput{}, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    pod,
		NewPod:    npod,
	}, nil
}

// get k8s.node of the new hosting node
func (r *ReScheduler) getNode(action *proto.ActionItemDTO) (*api.Node, error) {
	//1. check host entity
	hostSE := action.GetNewSE()
	if hostSE == nil {
		err := fmt.Errorf("New host entity is empty")
		glog.Errorf("%v.", err)
		return nil, err
	}

	//2. check entity type
	etype := hostSE.GetEntityType()
	if etype != proto.EntityDTO_VIRTUAL_MACHINE && etype != proto.EntityDTO_PHYSICAL_MACHINE {
		err := fmt.Errorf("The move destination [%v] is neither a VM nor a PM", etype)
		glog.Errorf("%v.", err)
		return nil, err
	}

	//3. get node from properties
	node, err := util.GetNodeFromProperties(r.clusterScraper.Clientset, hostSE.GetEntityProperties())
	if err == nil {
		glog.V(2).Infof("Get node(%v) from properties.", node.Name)
		return node, nil
	}

	//4. get node by displayName
	node, err = util.GetNodebyName(r.clusterScraper.Clientset, hostSE.GetDisplayName())
	if err == nil {
		glog.V(2).Infof("Get node(%v) by displayName.", node.Name)
		return node, nil
	}

	//5. get node by UUID
	node, err = util.GetNodebyUUID(r.clusterScraper.Clientset, hostSE.GetId())
	if err == nil {
		glog.V(2).Infof("Get node(%v) by UUID(%v).", node.Name, hostSE.GetId())
		return node, nil
	}

	//6. get node by IP
	vmIPs := getVMIps(hostSE)
	if len(vmIPs) > 0 {
		node, err = util.GetNodebyIP(r.clusterScraper.Clientset, vmIPs)
		if err == nil {
			glog.V(2).Infof("Get node(%v) by IP.", hostSE.GetDisplayName())
			return node, nil
		}
		err = fmt.Errorf("failed to get node %s by IP %+v: %v",
			hostSE.GetDisplayName(), vmIPs, err)
	} else {
		err = fmt.Errorf("failed to get node %s: IPs are empty",
			hostSE.GetDisplayName())
	}
	glog.Errorf("%v.", err)
	return nil, err
}

// get kubernetes pod, and the new hosting kubernetes node
func (r *ReScheduler) getPodNode(action *proto.ActionItemDTO) (*api.Node, error) {
	glog.V(4).Infof("MoveActionItem: %++v", action)
	// Check and find the new hosting node for the pod.
	return r.getNode(action)
}

// Check whether the action should be executed.
// TODO: find a reliable way to check node's status; current checking has no actual effect.
func (r *ReScheduler) preActionCheck(pod *api.Pod, node *api.Node) error {
	fullName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// Check if the pod privilege is supported
	if !util.SupportPrivilegePod(pod, r.sccAllowedSet) {
		err := fmt.Errorf("Pod %s has unsupported SCC", fullName)
		glog.Errorf("%v.", err)
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
		return nil, err
	}

	nodeName := node.Name
	fullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	// if the pod is already on the target node, then simply return success.
	if pod.Spec.NodeName == nodeName {
		return nil, fmt.Errorf("pod [%v] is already on host [%v]", fullName, nodeName)
	}

	ownerInfo, err := podutil.GetPodParentInfo(pod)
	if err != nil {
		return nil, fmt.Errorf("cannot get parent info of pod [%v]: %v", fullName, err)
	}

	if !util.SupportedParent(ownerInfo, false) {
		return nil, fmt.Errorf("the object kind [%v] of [%s] is not supported", ownerInfo.Kind, ownerInfo.Name)
	}
	//2. move
	return movePod(r.clusterScraper, pod, nodeName, ownerInfo.Kind,
		ownerInfo.Name, r.failureThreshold, r.failVolumePodMoves, r.updateQuotaToAllowMoves, r.lockMap)
}

func getVMIps(entity *proto.EntityDTO) []string {
	result := []string{}

	if entity.GetEntityType() != proto.EntityDTO_VIRTUAL_MACHINE {
		glog.Errorf("Hosting node is a not virtual machine: %++v", entity.GetEntityType())
		return result
	}

	vmData := entity.GetVirtualMachineData()
	if vmData == nil {
		err := fmt.Errorf("Missing virtualMachineData[%v] in targetSE", entity.GetDisplayName())
		glog.Error(err.Error())
		return result
	}

	if len(vmData.GetIpAddress()) < 1 {
		glog.Warningf("Machine IPs are empty: %++v", vmData)
	}

	return vmData.GetIpAddress()
}
