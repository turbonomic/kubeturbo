package executor

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	k8serror "k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/unversioned"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	// Set the grace period to 0 for deleting the pod immediately.
	podDeletionGracePeriod int64 = 0
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
		// If the targetObject doesn't have any parent object, use its namespace and name as key.
		key = fmt.Sprintf("%s/%s", actionContent.TargetObject.TargetObjectNamespace,
			actionContent.TargetObject.TargetObjectName)
	} else {
		return nil, errors.New("Failed to setup re-scheduler consumer: failed to retrieve the UID of " +
			"replication controller or replca set.")
	}
	glog.V(3).Infof("The current re-scheduler consumer is listening on the any pod created by "+
		"replication controller or replica set with UID  %s", key)
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
	grace := podDeletionGracePeriod
	deleteOption := &api.DeleteOptions{GracePeriodSeconds: &grace}
	err = r.kubeClient.Pods(originalPod.Namespace).Delete(originalPod.Name, deleteOption)
	if err != nil {
		return nil, fmt.Errorf("Failed to delete pod %s: %s", podIdentifier, err)
	}
	podDeletionChecker := &podDeletionChecker{r.kubeClient}
	if err := podDeletionChecker.check(originalPod); err != nil {
		return nil, fmt.Errorf("Failed to delete pod %s: %s", podIdentifier, err)
	}
	glog.V(3).Infof("Successfully delete pod %s.\n", podIdentifier)

	// 4. create a pending pod if necessary
	if actionContent.ParentObjectRef.ParentObjectUID == "" {
		glog.V(2).Infof("Pod %s a standalone pod. Need to clone manually", podIdentifier)
		podClone := &podClone{r.kubeClient}
		err := podClone.clone(originalPod)
		if err != nil {
			return nil, err
		}
	}

	// 5. Wait for desired pending pod
	// Set a timer for 5 minutes.
	t := time.NewTimer(secondPhaseTimeoutLimit)
	for {
		select {
		case p, ok := <-podConsumer.WaitPod():
			if !ok {
				return nil, errors.New("Failed to receive the pending pod generated as a result of " +
					"rescheduling.")
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

		case <-t.C:
			// timeout
			return nil, errors.New("Timed out at the second phase when try to finish the rescheduling " +
				"process")
		}
	}
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

// PodDeletionChecker is responsible for check is a given pod is actually deleted completely from cluster.
type podDeletionChecker struct {
	*client.Client
}

func (checker *podDeletionChecker) check(pod *api.Pod) error {
	t := time.NewTimer(podDeletionTimeout)
	for {
		select {
		case <-t.C:
			// timeout
			return fmt.Errorf("Timed out when waiting the original pod %s/%s to be deleted.",
				pod.Namespace, pod.Name)
		default:
			deleted, err := checker.isPodDeleted(pod)
			if !deleted {
				if err != nil {
					return err
				} else {
					time.Sleep(time.Second * 1)
				}
			} else {
				// the pod has been deleted, continue after for loop.
				glog.V(3).Infof("Break for loop.")
				return nil
			}
		}
	}
}

// Check if a pod with given namespace and name does not exist in the cluster.
func (checker *podDeletionChecker) isPodDeleted(pod *api.Pod) (bool, error) {
	currPod, err := checker.Pods(pod.Namespace).Get(pod.Name)
	if err != nil {
		err, ok := err.(*k8serror.StatusError)
		if !ok {
			return false, fmt.Errorf("Failed to check if %s/%s has been deleted: %s",
				pod.Namespace, pod.Name, err)
		}
		if err.Status().Reason != unversioned.StatusReasonNotFound {
			return false, fmt.Errorf("Failed to check if %s/%s has been deleted: %s",
				pod.Namespace, pod.Name, err)
		}
		// Now we assure the err is NOT FOUND, which indicate the pod has been deleted.
		glog.V(4).Infof("Cannnot find Pod %s/%s in the cluster. It's been deleted.", pod.Namespace, pod.Name)
		return true, nil
	}
	// If found pod, check if the new pod has the same name and namespace with the original pod.
	return currPod.UID != pod.UID, nil
}

// PodClone creates a clone of a given pod. The new Pod will have the same namespace and name,
// except that UID is different.
type podClone struct {
	*client.Client
}

func (pc *podClone) clone(originalPod *api.Pod) error {
	podNamespace := originalPod.Namespace
	podName := originalPod.Name

	pod := cloneHelper(originalPod)
	_, err := pc.Pods(podNamespace).Create(pod)
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
