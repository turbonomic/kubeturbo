package executor

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"github.com/vmturbo/kubeturbo/pkg/action/util"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
	"github.com/vmturbo/kubeturbo/pkg/turbostore"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ReScheduler struct {
	kubeClient *client.Client
	broker     turbostore.Broker
}

func NewReScheduler(client *client.Client, broker turbostore.Broker) *ReScheduler {
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
	podIdentifier := targetPod.GetId() // podIdentifier must have format as "Namespace:Name"
	originalPod, err := util.GetPodFromCluster(r.kubeClient, podIdentifier)
	if err != nil {
		return nil, fmt.Errorf("Try to move pod %s, but could not find it in the cluster.", podIdentifier)
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
	var parentObjRef *turboaction.ParentObjectRef
	// TODO, Ignore error?
	parentRefObject, err := probe.FindParentReferenceObject(originalPod)
	if err != nil {
		return nil, err
	}
	if parentRefObject != nil {
		parentObjRef = &turboaction.ParentObjectRef{
			ParentObjectUID:       string(parentRefObject.UID),
			ParentObjectNamespace: parentRefObject.Namespace,
			ParentObjectName:      parentRefObject.Name,
			ParentObjectType:      parentRefObject.Kind,
		}
	}
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

func (r *ReScheduler) reSchedule(action *turboaction.TurboAction) (*turboaction.TurboAction, error) {
	// 1. Setup consumer
	actionContent := action.Content
	// Move destination should not be empty
	moveSpec, ok := actionContent.ActionSpec.(turboaction.MoveSpec)
	if !ok || moveSpec.Destination == "" {
		return nil, errors.New("Failed to setup re-scheduler as move destination is nil after pre-precess.")
	}

	var key string
	if actionContent.ParentObjectRef.ParentObjectUID != "" {
		key = actionContent.ParentObjectRef.ParentObjectUID
	} else if actionContent.TargetObject.TargetObjectUID != "" {
		key = actionContent.TargetObject.TargetObjectUID
	} else {
		return nil, errors.New("Failed to setup re-scheduler consumer: failed to retrieve the key.")
	}
	glog.V(3).Infof("The current re-scheduler consumer is listening on the key %s", key)
	podConsumer := turbostore.NewPodConsumer(string(action.UID), key, r.broker)

	// 2. Get the target Pod
	targetObj := actionContent.TargetObject
	originalPod, err := r.kubeClient.Pods(targetObj.TargetObjectNamespace).Get(targetObj.TargetObjectName)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the pod to be re-scheduled based on the given targetObject "+
			"info: %++v", targetObj)
	}
	podIdentifier := util.BuildIdentifier(originalPod.Namespace, originalPod.Name)

	// 3. delete pod
	// TODO, we may want to specify a DeleteOption.
	err = r.kubeClient.Pods(originalPod.Namespace).Delete(originalPod.Name, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to delete pod %s: %s", podIdentifier, err)
	}
	glog.V(3).Infof("Successfully delete pod %s.\n", podIdentifier)

	// 4. create a pending pod if necessary
	if actionContent.ParentObjectRef.ParentObjectUID == "" {
		glog.V(2).Infof("Pod %s a standalone pod. Need to clone manually", podIdentifier)
		// TODO need to wait the pod has been deleted complete.. A better way may related to DeleteOption?
		time.Sleep(time.Second * 3)
		podClone := &podClone{r.kubeClient}
		err := podClone.clone(originalPod)
		if err != nil {
			return nil, err
		}
	}

	// 5. Wait for desired pending pod
	// TODO, should we always block here?
	p, ok := <-podConsumer.WaitPod()
	if !ok {
		return nil, errors.New("Failed to receive the pending pod generated as a result of rescheduling.")
	}
	podConsumer.Leave(key, r.broker)


	// 6. Received the pod, start 2nd stage.
	err = r.reSchedulePodToDestination(p, moveSpec.Destination)
	if err != nil {
		return nil, fmt.Errorf("Re-schudeling failed at the 2nd stage: %s", err)
	}


	// 7. Update turbo action.
	moveSpec.NewObjectName = p.Name
	moveSpec.NewObjectNamespace = p.Namespace
	actionContent.ActionSpec = moveSpec
	action.Content = actionContent
	action.Status = turboaction.Executed

	return action, nil
}

// Extract the reschedule destination from action and then bind the pod to it.
func (r *ReScheduler) reSchedulePodToDestination(pod *api.Pod, destination string) error {
	glog.V(2).Infof("Pod %s/%s is to be scheduled to %s as a result of MOVE action",
		pod.Namespace, pod.Name, destination)

	b := &api.Binding{
		ObjectMeta: api.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name},
		Target: api.ObjectReference{
			Kind: "Node",
			Name: destination,
		},
	}
	binder := &binder{r.kubeClient}

	err := binder.Bind(b)
	if err != nil {
		glog.V(1).Infof("Failed to bind pod: %+v", err)
		return err
	}
	return nil
}

type podClone struct {
	*client.Client
}

// verify the pod has been deleted and then create a clone.
func (pc *podClone) clone(originalPod *api.Pod) error {
	podNamespace := originalPod.Namespace
	podName := originalPod.Name
	_, err := pc.Pods(podNamespace).Get(podName)
	if err != nil {
		// TODO, is this the only reason to get the error?
		glog.V(3).Infof("%s has can no long be found in the cluster. This is expected.")
	}

	pod := cloneHelper(originalPod)
	_, err = pc.Pods(podNamespace).Create(pod)
	if err != nil {
		glog.Errorf("Error recreating pod %s/%s: %s", podNamespace, podName, err)
		return err
	}

	return nil
}

// Create a new pod instance based on the given pod.
// All the necessary fields is copied to the new Pod instance except the NodeName in PodSpec.
func cloneHelper(podToCopy *api.Pod) *api.Pod {
	pod := &api.Pod{}
	pod.Name = podToCopy.Name
	pod.GenerateName = podToCopy.GenerateName
	pod.Namespace = podToCopy.Namespace
	pod.Labels = podToCopy.Labels
	pod.Annotations = podToCopy.Annotations
	pod.Spec = podToCopy.Spec
	pod.Spec.NodeName = ""
	glog.V(4).Infof("Copied pod is: %++v", pod)

	return pod
}
