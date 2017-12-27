package executor

import (
	"fmt"
	"github.com/golang/glog"
	"strings"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ReScheduler struct {
	kubeClient                 *kclient.Clientset
	k8sVersion                 string
	noneSchedulerName          string
	stitchType                 stitching.StitchingPropertyType
	enableNonDisruptiveSupport bool

	lockMap *util.ExpirationMap
}

func NewReScheduler(client *kclient.Clientset, k8sver, noschedulerName string, lmap *util.ExpirationMap, stype stitching.StitchingPropertyType, enableNonDisruptiveSupport bool) *ReScheduler {
	return &ReScheduler{
		kubeClient:                 client,
		k8sVersion:                 k8sver,
		noneSchedulerName:          noschedulerName,
		lockMap:                    lmap,
		stitchType:                 stype,
		enableNonDisruptiveSupport: enableNonDisruptiveSupport,
	}
}

func (r *ReScheduler) Execute(actionItem *proto.ActionItemDTO) error {
	if actionItem == nil {
		return fmt.Errorf("ActionItem passed in is nil")
	}

	//1. get target Pod and new hosting Node
	pod, node, err := r.getPodNode(actionItem)
	if err != nil {
		return err
	}

	//2. move pod to the node
	return r.reSchedule(pod, node)
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
		err := fmt.Errorf("new hosting node is empty.")
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

	//1. If Pod is terminated, then no need to move it.
	// if pod.Status.Phase != api.PodRunning {
	if pod.Status.Phase == api.PodSucceeded {
		glog.Errorf("Move action should be aborted: original pod termiated:%v phase:%v", fullName, pod.Status.Phase)
	}

	//2. if Node is out of condition
	if node.Status.Phase != api.NodeRunning {
		glog.Errorf("Move action should be aborted: pod[%v]'s new destination host is not running: %v", fullName, node.Status.Phase)
	}

	return nil
}

func (r *ReScheduler) reSchedule(pod *api.Pod, node *api.Node) error {
	//1. do some check
	if err := r.preActionCheck(pod, node); err != nil {
		glog.Errorf("Move action aborted: %v", err)
		return fmt.Errorf("Failed")
	}

	nodeName := node.Name
	fullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	// if the pod is already on the target node, then simply return success.
	if pod.Spec.NodeName == nodeName {
		glog.V(2).Infof("Move action aborted: pod[%v] is already on host[%v].", fullName, nodeName)
		return nil
	}

	//2. move
	parentKind, parentName, err := util.GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("Move action aborted: cannot get pod-%v parent info: %v", fullName, err)
		return fmt.Errorf("Failed")
	}

	if parentKind == "" {
		_, err = r.moveBarePod(pod, nodeName)
	} else {
		_, err = r.moveControllerPod(pod, parentKind, parentName, nodeName)
	}
	if err != nil {
		glog.Errorf("move pod [%s] failed: %v", fullName, err)
		return fmt.Errorf("Failed")
	}

	//3. check
	glog.V(2).Infof("Begin to check moveAction for pod[%v]", fullName)
	if err = r.checkPod(pod, nodeName); err != nil {
		glog.Errorf("Checking moveAction failed: pod[%v] failed: %v", fullName, err)
		return fmt.Errorf("Failed")
	}
	glog.V(2).Infof("Checking moveAction succeeded: pod[%v] is on node[%v].", fullName, nodeName)

	return nil
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
	if err := helper.SetupLock(r.lockMap); err != nil {
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

	if r.enableNonDisruptiveSupport {
		// Performs operations for actions to be non-disruptive (when the pod is the only one for the controller)
		// NOTE: It doesn't support the case of the pod associated to Deployment in version lower than 1.6.0.
		//       In such case, the action execution will fail.
		contKind, contName, err := util.GetPodGrandInfo(r.kubeClient, pod)
		if err != nil {
			return nil, err
		}
		nonDisruptiveHelper := NewNonDisruptiveHelper(r.kubeClient, pod.Namespace, contKind, contName, pod.Name, r.k8sVersion)

		// Performs operations for non-disruptive move actions
		if err := nonDisruptiveHelper.OperateForNonDisruption(); err != nil {
			glog.V(3).Infof("Move pod[%s] failed: Failed to perform non-disruptive operations", pod.Name)
			return nil, err
		}

		// Performs cleanup for non-disruptive move actions
		defer nonDisruptiveHelper.CleanUp()
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

// as there may be concurrent actions on the same bare pod:
//   one action is to move Pod, and the other is to Resize Pod.container;
// thus, concurrent control should also be applied to bare pods.
func (r *ReScheduler) moveBarePod(pod *api.Pod, nodeName string) (*api.Pod, error) {
	podkey := util.BuildIdentifier(pod.Namespace, pod.Name)
	// 1. setup lockHelper
	helper, err := util.NewLockHelper(podkey, r.lockMap)
	if err != nil {
		return nil, err
	}

	// 2. wait to get a lock of current Pod
	timeout := defaultWaitLockTimeOut
	interval := defaultWaitLockSleep
	err = helper.Trylock(timeout, interval)
	if err != nil {
		glog.Errorf("move pod[%s] failed: failed to acquire lock of pod[%s]", podkey, podkey)
		return nil, err
	}
	defer helper.ReleaseLock()

	// 3. move the Pod
	helper.KeepRenewLock()
	return movePod(r.kubeClient, pod, nodeName, defaultRetryLess)
}

// check the liveness of pod, and the hosting Node
// return (retry, error)
func doCheckPodNode(client *kclient.Clientset, namespace, name, nodeName string) (bool, error) {
	pod, err := util.GetPod(client, namespace, name)
	if err != nil {
		return true, err
	}

	phase := pod.Status.Phase
	if phase == api.PodRunning {
		if strings.EqualFold(pod.Spec.NodeName, nodeName) {
			return false, nil
		}
		return false, fmt.Errorf("running on a unexpected node[%v].", pod.Spec.NodeName)
	}

	if pod.DeletionGracePeriodSeconds != nil {
		return false, fmt.Errorf("Pod is being deleted.")
	}

	return true, fmt.Errorf("pod is not in running phase[%v] yet.", phase)
}

func (r *ReScheduler) checkPod(pod *api.Pod, nodeName string) error {
	retryNum := defaultRetryMore
	interval := defaultPodCreateSleep
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
