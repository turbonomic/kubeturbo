package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	//"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"strings"
)

const (
	// Set the grace period to 0 for deleting the pod immediately.
	podDeletionGracePeriod        int64 = 0
	DefaultNoneExistSchedulerName       = "turbo-none-exist-scheduler"
	KindReplicationController           = "ReplicationController"
	KindReplicaSet                      = "ReplicaSet"
)

type ReScheduler struct {
	kubeClient *client.Clientset
	broker     turbostore.Broker
}

func NewReScheduler(client *client.Clientset, broker turbostore.Broker) *ReScheduler {
	return &ReScheduler{
		kubeClient: client,
		broker:     broker,
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
		return nil, fmt.Errorf("re-schedule failed: get original pod:%v\n%v", fullName, err.Error())
	}

	// If Pod is terminated, then no need to move it.
	// if pod.Status.Phase != api.PodRunning {
	if pod.Status.Phase == api.PodSucceeded {
		return nil, fmt.Errorf("re-schedule failed: original pod termiated:%v phase:%v", fullName, pod.Status.Phase)
	}

	// If Pod is already in the destination Node, then no need to move it.
	if strings.EqualFold(nodeName, pod.Spec.NodeName) {
		return nil, fmt.Errorf("re-schedule failed: pod is already in Destination Pod-%v on %v", fullName, pod.Spec.NodeName)
	}

	return pod, nil
}

func (r *ReScheduler) reSchedule(action *turboaction.TurboAction) (*turboaction.TurboAction, error) {
	actionContent := action.Content
	moveSpec, ok := actionContent.ActionSpec.(turboaction.MoveSpec)
	if !ok || moveSpec.Destination == "" {
		return nil, fmt.Errorf("re-scheduler failed: destination is nil after pre-precess.")
	}

	//1. get original Pod
	podName := actionContent.TargetObject.TargetObjectName
	namespace := actionContent.TargetObject.TargetObjectNamespace
	nodeName := moveSpec.Destination
	fullName := fmt.Sprintf("%s/%s", namespace, podName)

	pod, err := r.preActionCheck(podName, namespace, nodeName)
	if err != nil {
		return nil, err
	}

	//2. update schedulerName of parent object
	parentKind, parentName, err := getParentInfo(pod)
	if err != nil {
		return nil, fmt.Errorf("move-abort: cannot get pod-%v parent info: %v", fullName, err.Error())
	}

	var f func(*client.Clientset, string, string, string, string) (string, error)
	switch parentKind {
	case "":
		glog.V(3).Infof("pod-%v is a standalone Pod, move it directly.", fullName)
		f = func(c *client.Clientset, ns, pname, cname, sname string) (string, error) { return "", nil }
	case KindReplicationController:
		glog.V(3).Infof("pod-%v parent is a ReplicationController-%v", fullName, parentName)
		f = updateRCscheduler
	case KindReplicaSet:
		glog.V(3).Infof("pod-%v parent is a ReplicaSet-%v", fullName, parentName)
		f = updateRSscheduler
	default:
		err = fmt.Errorf("unsupported parent-[%v] Kind-[%v]", parentName, parentKind)
		glog.Warning(err.Error())
		return nil, err
	}

	preScheduler, err := f(r.kubeClient, namespace, parentName, "", DefaultNoneExistSchedulerName)
	if err != nil {
		err = fmt.Errorf("move-failed: update pod-%v parent-%v scheduler failed:%v", fullName, parentName, err.Error())
		glog.Error(err.Error())
		return nil, err
	}
	defer f(r.kubeClient, namespace, parentName, DefaultNoneExistSchedulerName, preScheduler)

	//3. move the Pod
	npod, err := movePod(r.kubeClient, pod, nodeName)
	if err != nil {
		return nil, fmt.Errorf("re-schedule failed: failed to create new Pod: %v\n%v", fullName, err.Error())
	}

	//4. update moveAction
	moveSpec.NewObjectName = npod.Name
	moveSpec.NewObjectNamespace = npod.Namespace
	action.Content.ActionSpec = moveSpec
	action.Status = turboaction.Executed

	return action, nil
}

//update the schedulerName of a ReplicaSet to schedulerName.
// if condName is not empty, then only current schedulerName is same to condName, then will do the update.
// return the previous schedulerName
func updateRSscheduler(client *client.Clientset, nameSpace, rsName, condName, schedulerName string) (string, error) {
	currentName := ""
	if schedulerName == "" {
		return "", fmt.Errorf("update failed: schedulerName is empty")
	}

	rsClient := client.ExtensionsV1beta1().ReplicaSets(nameSpace)
	if rsClient == nil {
		return "", fmt.Errorf("failed to get ReplicaSet client in namespace: %v", nameSpace)
	}

	id := fmt.Sprintf("%v/%v", nameSpace, rsName)

	//1. get ReplicaSet
	option := metav1.GetOptions{}
	rs, err := rsClient.Get(rsName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicaSet-%v: %v", id, err.Error())
		glog.Error(err.Error())
		return currentName, err
	}

	//2. check whether to do the update
	currentName = rs.Spec.Template.Spec.SchedulerName
	if currentName == schedulerName {
		glog.V(3).Infof("No need to update: schedulerName is already is [%v]-[%v]", id, schedulerName)
		return "", nil
	}
	if condName != "" && currentName != condName {
		err := fmt.Errorf("abort to update schedulerName; [%v] - [%v] Vs. [%v]", id, condName, currentName)
		glog.Warning(err.Error())
		return "", err
	}

	//3. update schedulerName
	rs.Spec.Template.Spec.SchedulerName = schedulerName
	_, err = rsClient.Update(rs)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v:%v\n", id, err.Error())
		glog.Error(err.Error())
		return currentName, err
	}

	//4. check final status
	rs, err = rsClient.Get(rsName, option)
	if err != nil {
		err = fmt.Errorf("failed to check ReplicaSet-%v: %v", id, err.Error())
		return currentName, err
	}

	if rs.Spec.Template.Spec.SchedulerName != schedulerName {
		err = fmt.Errorf("failed to update schedulerName for ReplicaSet-%v: %v", id, err.Error())
		glog.Error(err.Error())
		return "", err
	}

	glog.V(2).Infof("Successfully update ReplicationController:%v scheduler name [%v] -> [%v]", id, currentName,
		schedulerName)

	return currentName, nil
}

//update the schedulerName of a ReplicationController
// if condName is not empty, then only current schedulerName is same to condName, then will do the update.
// return the previous schedulerName
func updateRCscheduler(client *client.Clientset, nameSpace, rcName, condName, schedulerName string) (string, error) {
	currentName := ""
	if schedulerName == "" {
		return "", fmt.Errorf("update failed: schedulerName is empty")
	}

	id := fmt.Sprintf("%v/%v", nameSpace, rcName)
	rcClient := client.CoreV1().ReplicationControllers(nameSpace)

	//1. get
	option := metav1.GetOptions{}
	rc, err := rcClient.Get(rcName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicationController-%v: %v\n", id, err.Error())
		glog.Error(err.Error())
		return currentName, err
	}

	//2. check whether to update
	currentName = rc.Spec.Template.Spec.SchedulerName
	if currentName == schedulerName {
		glog.V(3).Infof("No need to update: schedulerName is already is [%v]-[%v]", id, schedulerName)
		return "", nil
	}
	if condName != "" && currentName != condName {
		err := fmt.Errorf("abort to update schedulerName; [%v] - [%v] Vs. [%v]", id, condName, currentName)
		glog.Warning(err.Error())
		return "", err
	}

	//3. update
	rc.Spec.Template.Spec.SchedulerName = schedulerName
	rc, err = rcClient.Update(rc)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v:%v\n", id, err.Error())
		glog.Error(err.Error())
		return currentName, err
	}

	//4. check final status
	rc, err = rcClient.Get(rcName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicationController-%v: %v\n", id, err.Error())
		glog.Error(err.Error())
		return currentName, err
	}

	if rc.Spec.Template.Spec.SchedulerName != schedulerName {
		err = fmt.Errorf("failed to update schedulerName for ReplicaController-%v: %v", id, err.Error())
		glog.Error(err.Error())
		return "", err
	}

	glog.V(2).Infof("Successfully update ReplicationController:%v scheduler name from [%v] to [%v]", id, currentName,
		schedulerName)

	return currentName, nil
}

// move pod nameSpace/podName to node nodeName
func movePod(client *client.Clientset, pod *api.Pod, nodeName string) (*api.Pod, error) {
	podClient := client.CoreV1().Pods(pod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("cannot get Pod client for nameSpace:%v", pod.Namespace)
		glog.Errorf(err.Error())
		return nil, err
	}

	//1. copy the original pod, and set the nodeName
	id := fmt.Sprintf("%v/%v", pod.Namespace, pod.Name)
	glog.V(2).Infof("move-pod: begin to move %v from %v to %v", id, pod.Spec.NodeName, nodeName)

	npod := &api.Pod{}
	copyPodInfo(pod, npod)
	npod.Spec.NodeName = nodeName

	//2. kill original pod
	//TODO: find the reason why it does not work when grace > 0
	//var grace int64 = *pod.Spec.TerminationGracePeriodSeconds
	var grace int64 = 0
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	err := podClient.Delete(pod.Name, delOption)
	if err != nil {
		err = fmt.Errorf("move-failed: failed to delete original pod-%v: %v", id, err.Error())
		glog.Error(err.Error())
		return nil, err
	}

	//3. create (and bind) the new Pod
	//time.Sleep(time.Duration(grace) * time.Second)
	_, err = podClient.Create(npod)
	if err != nil {
		err = fmt.Errorf("move-failed: failed to create new pod-%v: %v", id, err.Error())
		glog.Error(err.Error())
		return nil, err
	}

	glog.V(2).Infof("move-finished: %v from %v to %v", id, pod.Spec.NodeName, nodeName)

	return npod, nil
}

func getParentInfo(pod *api.Pod) (string, string, error) {
	//1. check ownerReferences:
	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		for _, owner := range pod.OwnerReferences {
			if *owner.Controller {
				return owner.Kind, owner.Name, nil
			}
		}
	}

	glog.V(4).Infof("cannot find pod-%v/%v parent by OwnerReferences.", pod.Namespace, pod.Name)

	//2. check annotations:
	if pod.Annotations != nil && len(pod.Annotations) > 0 {
		key := "kubernetes.io/created-by"
		if value, ok := pod.Annotations[key]; ok {

			var ref api.SerializedReference

			if err := json.Unmarshal([]byte(value), ref); err != nil {
				err = fmt.Errorf("failed to decode parent annoation:%v", err.Error())
				return "", "", err
			}

			return ref.Reference.Kind, ref.Reference.Name, nil
		}
	}

	glog.V(4).Infof("cannot find pod-%v/%v parent by Annotations.", pod.Namespace, pod.Name)

	return "", "", nil
}

func copyPodInfo(oldPod, newPod *api.Pod) {
	//1. typeMeta
	newPod.TypeMeta = oldPod.TypeMeta

	//2. objectMeta
	newPod.ObjectMeta = oldPod.ObjectMeta
	newPod.SelfLink = ""
	newPod.ResourceVersion = ""
	newPod.Generation = 0
	newPod.CreationTimestamp = metav1.Time{}
	newPod.DeletionTimestamp = nil
	newPod.DeletionGracePeriodSeconds = nil

	//3. podSpec
	spec := oldPod.Spec
	spec.Hostname = ""
	spec.Subdomain = ""
	spec.NodeName = ""

	newPod.Spec = spec
	return
}
