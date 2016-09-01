package action

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/registry"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// KubernetesActionExecutor is responsilbe for executing different kinds of actions requested by vmt server.
type Rescheduler struct {
	kubeClient       *client.Client
	vmtEventRegistry *registry.VMTEventRegistry
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewRescheduler(client *client.Client, vmtEventRegistry *registry.VMTEventRegistry) *Rescheduler {
	return &Rescheduler{
		kubeClient:       client,
		vmtEventRegistry: vmtEventRegistry,
	}
}

func (this *Rescheduler) MovePod(actionItem *proto.ActionItemDTO, msgID int32) (*registry.VMTEvent, error) {
	if actionItem == nil {
		return nil, fmt.Errorf("ActionItem passed in is nil")
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
				return nil, fmt.Errorf("Missing VirtualMachineData in ActionItemDTO from server")
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
			return nil, fmt.Errorf("Miss IP addresses in ActionItemDTO.")
		}

		// Get the actual node name from Kubernetes cluster based on IP address.
		nodeIdentifier, err := GetNodeNameFromIP(this.kubeClient, machineIPs)
		if err != nil {
			return nil, err
		}

		targetPod := actionItem.GetTargetSE()
		podIdentifier := targetPod.GetId() // podIdentifier must have format as "Namespace/Name"

		originalPod, err := GetPodFromCluster(this.kubeClient, podIdentifier)
		if err != nil {
			return nil, err
		}

		content := registry.NewVMTEventContentBuilder(ActionMove, originalPod.Name, int(msgID)).
			MoveSpec(originalPod.Spec.NodeName, nodeIdentifier).Build()
		event := registry.NewVMTEventBuilder(originalPod.Namespace).Content(content).Create()
		glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

		// Create VMTEvent and post onto etcd.
		_, errorPost := this.vmtEventRegistry.Create(&event)
		if errorPost != nil {
			return nil, fmt.Errorf("Error posting VMTEvent %s for %s", content.ActionType, content.TargetSE)
		}
		err = this.ReschedulePod(originalPod, podIdentifier, nodeIdentifier, msgID)
		if err != nil {
			return nil, fmt.Errorf("Error moving Pod %s: %s", podIdentifier, err)
		}

		return &event, nil
	} else {
		return nil, fmt.Errorf("The target service entity for move destiantion is neither a VM nor a PM.")
	}
}

// Reschdule is such an action that should be excuted in the following order:
// 1. Delete the Pod to be moved.
// 2. Replication controller will automatically create a new replica and post to api server.
// 3. At the same time create a VMTEvent, containing move action info, and post it to etcd.
// 4. Pod watcher in the vmturbo-service find the new Pod and the new VMTEvent.
// 5. Schedule Pod according to move action.
// This method delete the pod and create a VMTEvent.
func (this *Rescheduler) ReschedulePod(originalPod *api.Pod, podIdentifier, targetNodeIdentifier string, msgID int32) error {

	if targetNodeIdentifier == "" {
		return fmt.Errorf("Target node identifier should not be empty.\n")
	}

	podNamespace := originalPod.Namespace
	podName := originalPod.Name

	glog.V(3).Infof("Now Move Pod %s.", podIdentifier)

	rcForPod, err := FindReplicationControllerForPod(this.kubeClient, originalPod)
	if err != nil {
		glog.Errorf("Error getting Replication for Pod %s: %s\n", podIdentifier, err)
	}

	// Delete pod
	err = this.kubeClient.Pods(podNamespace).Delete(podName, nil)
	if err != nil {
		glog.Errorf("Error deleting pod %s: %s.\n", podIdentifier, err)
		return fmt.Errorf("Error deleting pod %s: %s.\n", podIdentifier, err)
	} else {
		glog.V(3).Infof("Successfully delete pod %s.\n", podIdentifier)
	}

	// This is a standalone pod. Need to clone manually.
	if rcForPod == nil {
		time.Sleep(time.Second * 3)
		return this.recreatePod(originalPod)
	}
	return nil
}

// verify the pod has been deleted and then create a clone.
func (this *Rescheduler) recreatePod(originalPod *api.Pod) error {
	podNamespace := originalPod.Namespace
	podName := originalPod.Name
	_, err := this.kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.V(3).Infof("Get deleted standalone Pod with error %s. This is expected.", err)
	}

	pod := clonePod(originalPod)
	_, err = this.kubeClient.Pods(podNamespace).Create(pod)
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
