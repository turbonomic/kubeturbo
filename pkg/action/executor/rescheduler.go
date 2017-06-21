package executor

import (
	"errors"
	"fmt"

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
	podDeletionGracePeriod int64 = 0
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

func (r *ReScheduler) preActionCheck(moveSpec *turboaction.MoveSpec, pod *api.Pod) error {
	// if pod.Status.Phase != api.PodRunning {
	if pod.Status.Phase == api.PodSucceeded {
		err := fmt.Errorf("re-schedule failed: original pod termiated:%v/%v",
			pod.Namespace, pod.Name)
		return err
	}

	if strings.EqualFold(moveSpec.Destination, pod.Spec.NodeName) {
		err := fmt.Errorf("re-schedule failed: pod is already in Destination Pod: %v/%v on %v",
		pod.Namespace, pod.Name, pod.Spec.NodeName)
		return err
	}

	return nil
}

func (r *ReScheduler) reSchedule(action *turboaction.TurboAction) (*turboaction.TurboAction, error) {
	actionContent := action.Content
	moveSpec, ok := actionContent.ActionSpec.(turboaction.MoveSpec)
	if !ok || moveSpec.Destination == "" {
		return nil, fmt.Errorf("Failed to setup re-scheduler as move destination is nil after pre-precess.")
	}

	//1. get target Pod
	targetObj := actionContent.TargetObject
	ns := targetObj.TargetObjectNamespace
	podClient := r.kubeClient.CoreV1().Pods(ns)
	fullName := fmt.Sprintf("%s/%s", ns, targetObj.TargetObjectName)

	getOption = metav1.GetOptions{}
	pod, err := podClient.Get(targetObj.TargetObjectName, getOption)
	if err != nil {
		err = fmt.Errorf("re-schedule failed: get original pod:%v\n%v",
			fullName, err.Error())
		return nil, err
	}

	if err = r.preActionCheck(&moveSpec, pod); err != nil {
		return nil, err
	}

	//2. copy and kill current Pod
	npod := &api.Pod{}
	copyPodInfo(pod, npod)
	//TODO: if we can generate a new name, then we don't have to set graceTime=0.
	//npod.Name := genNewName(pod.Name)
	npod.Spec.NodeName = moveSpec.Destination

	grace := podDeletionGracePeriod
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	err = podClient.Delete(pod.Name, delOption)
	if err != nil {
		err = fmt.Errorf("re-schedule failed: failed to delete orginal Pod:%v\n%v",
			fullName, err.Error())
		return nil, err
	}

	//3. create (and bind) the new Pod
	_, err = podClient.Create(npod)
	if err != nil {
		err = fmt.Errorf("re-schedule failed: failed to create new Pod: %v\n%v",
			fullName, err.Error())
		return nil, err
	}

	//4. update moveAction
	moveSpec.NewObjectName = npod.Name
	moveSpec.NewObjectNamespace = npod.Namespace
	action.Content.ActionSpec = moveSpec
	action.Status = turboaction.Executed

	return action, nil
}

func copyPodInfo(oldPod, newPod *api.Pod) {
	//typeMeta
	newPod.Kind = oldPod.Kind
	newPod.APIVersion = oldPod.APIVersion

	//objectMeta
	newPod.Name = oldPod.Name
	newPod.Namespace = oldPod.Namespace
	newPod.Labels = oldPod.Labels
	newPod.GenerateName = oldPod.GenerateName
	newPod.Annotations = oldPod.Annotations
	newPod.OwnerReferences = oldPod.OwnerReferences
	newPod.Finalizers = oldPod.Finalizers
	newPod.ClusterName = oldPod.ClusterName
	newPod.UID = oldPod.UID

	//podSpec
	spec := oldPod.Spec
	spec.Hostname = ""
	spec.Subdomain = ""
	spec.NodeName = ""

	newPod.Spec = spec

	//won't copy status
	return
}
