package executor

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"github.com/vmturbo/kubeturbo/pkg/action/util"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"errors"
)

type Rescheduler struct {
	kubeClient *client.Client
}

// Create new VMT Actor. Must specify the Kubernetes client.
func NewRescheduler(client *client.Client) *Rescheduler {
	return &Rescheduler{
		kubeClient: client,
	}
}

func (r *Rescheduler) MovePod(actionItem *proto.ActionItemDTO, msgID int32) (*turboaction.TurboAction, error) {
	if actionItem == nil {
		return nil, errors.New("ActionItem passed in is nil")
	}
	newSEType := actionItem.GetNewSE().GetEntityType()
	if newSEType == proto.EntityDTO_VIRTUAL_MACHINE || newSEType == proto.EntityDTO_PHYSICAL_MACHINE {
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
			//TODO
			// machineIPS = <valid physical machine IP>
			break
		}

		glog.V(3).Infof("The IPs of targetNode is %v", machineIPs)
		if machineIPs == nil {
			return nil, errors.New("Miss IP addresses in ActionItemDTO.")
		}

		// Get the actual node name from Kubernetes cluster based on IP address.
		nodeIdentifier, err := util.GetNodeNameFromIP(r.kubeClient, machineIPs)
		if err != nil {
			return nil, err
		}

		targetPod := actionItem.GetTargetSE()
		podIdentifier := targetPod.GetId() // podIdentifier must have format as "Namespace/Name"

		originalPod, err := util.GetPodFromCluster(r.kubeClient, podIdentifier)
		if err != nil {
			return nil, err
		}

		var isStandAlone bool
		var parentObjRef *turboaction.ParentObjectRef
		parentRefObject, _ := probe.FindParentReferenceObject(originalPod)
		if parentRefObject != nil {
			parentObjRef = &turboaction.ParentObjectRef{
				parentRefObject.Name,
				parentRefObject.Kind,
			}
			isStandAlone = false
		} else {
			isStandAlone = true
		}
		targetObj := &turboaction.TargetObject{
			originalPod.Name,
			turboaction.TypePod,
		}
		moveSpec := turboaction.MoveSpec{
			Source:      originalPod.Spec.NodeName,
			Destination: nodeIdentifier,
		}
		content := turboaction.NewVMTEventContentBuilder(turboaction.ActionMove, targetObj, int(msgID)).ActionSpec(moveSpec).ParentObjectRef(parentObjRef).Build()
		event := turboaction.NewVMTEventBuilder(originalPod.Namespace).Content(content).Create()
		glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

		err = r.ReschedulePod(originalPod, nodeIdentifier, isStandAlone)
		if err != nil {
			return nil, fmt.Errorf("Error moving Pod %s: %s", podIdentifier, err)
		}

		return &event, nil
	} else {
		return nil, errors.New("The target service entity for move destiantion is neither a VM nor a PM.")
	}
}

// Reschdule is such an action that should be excuted in the following order:
// 1. Delete the Pod to be moved.
// 2. Replication controller will automatically create a new replica and post to api server.
// 3. Pod watcher in the vmturbo-service find the new Pod and the new VMTEvent.
// 4. Schedule Pod according to move action.
// This method delete the pod and create a VMTEvent.
func (r *Rescheduler) ReschedulePod(originalPod *api.Pod, targetNodeIdentifier string, isStandAlone bool) error {

	if targetNodeIdentifier == "" {
		return errors.New("Target node identifier should not be empty.\n")
	}

	podNamespace := originalPod.Namespace
	podName := originalPod.Name
	podIdentifier := podNamespace + "/" + podName

	glog.V(3).Infof("Now Move Pod %s.", podIdentifier)

	// Delete pod
	err := r.kubeClient.Pods(podNamespace).Delete(podName, nil)
	if err != nil {
		glog.Errorf("Error deleting pod %s: %s.\n", podIdentifier, err)
		return fmt.Errorf("Error deleting pod %s: %s.\n", podIdentifier, err)
	} else {
		glog.V(3).Infof("Successfully delete pod %s.\n", podIdentifier)
	}

	if isStandAlone {
		glog.Warningf("Cannot find replication controller or deployment related to the pod %s", podIdentifier)
		// This is a standalone pod. Need to clone manually.
		time.Sleep(time.Second * 3)
		return r.recreatePod(originalPod)
	}
	// If not a standalone pod, a new pod will be created by parent Object(ReplicationController, ReplicaSet) automatically.
	return nil
}

// verify the pod has been deleted and then create a clone.
func (r *Rescheduler) recreatePod(originalPod *api.Pod) error {
	podNamespace := originalPod.Namespace
	podName := originalPod.Name
	_, err := r.kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.V(3).Infof("Get deleted standalone Pod with error %s. This is expected.", err)
	}

	pod := clonePod(originalPod)
	_, err = r.kubeClient.Pods(podNamespace).Create(pod)
	if err != nil {
		glog.Errorf("Error recreating pod %s/%s: %s", podNamespace, podName, err)
		return err
	}
	return nil
}

func clonePod(podToCopy *api.Pod) *api.Pod {
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
