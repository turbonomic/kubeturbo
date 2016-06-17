package action

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

// KubernetesActionExecutor is responsilbe for executing different kinds of actions requested by vmt server.
type Rescheduler struct {
	kubeClient  *client.Client
	etcdStorage storage.Storage
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewRescheduler(client *client.Client, etcdStorage storage.Storage) *Rescheduler {
	return &Rescheduler{
		kubeClient:  client,
		etcdStorage: etcdStorage,
	}
}

func (this *Rescheduler) MovePod(actionItem *sdk.ActionItemDTO, msgID int32) error {
	if actionItem == nil {
		return fmt.Errorf("ActionItem passed in is nil")
	}
	newSEType := actionItem.GetNewSE().GetEntityType()
	if newSEType == sdk.EntityDTO_VIRTUAL_MACHINE || newSEType == sdk.EntityDTO_PHYSICAL_MACHINE {
		targetNode := actionItem.GetNewSE()

		var machineIPs []string

		switch newSEType {
		case sdk.EntityDTO_VIRTUAL_MACHINE:
			// K8s uses Ip address as the Identifier. The VM name passed by actionItem is the display name
			// that discovered by hypervisor. So here we must get the ip address from virtualMachineData in
			// targetNode entityDTO.
			vmData := targetNode.GetVirtualMachineData()
			if vmData == nil {
				return fmt.Errorf("Missing VirtualMachineData in ActionItemDTO from server")
			}
			machineIPs = vmData.GetIpAddress()
			break
		case sdk.EntityDTO_PHYSICAL_MACHINE:
			//TODO
			// machineIPS = <valid physical machine IP>
			break
		}

		glog.V(3).Infof("The IPs of targetNode is %v", machineIPs)
		if machineIPs == nil {
			return fmt.Errorf("Miss IP addresses in ActionItemDTO.")
		}

		// Get the actual node name from Kubernetes cluster based on IP address.
		nodeIdentifier, err := GetNodeNameFromIP(this.kubeClient, machineIPs)
		if err != nil {
			return err
		}

		targetPod := actionItem.GetTargetSE()
		podIdentifier := targetPod.GetId() // podIdentifier must have format as "Namespace/Name"

		err = this.ReschedulePod(podIdentifier, nodeIdentifier, msgID)
		if err != nil {
			return fmt.Errorf("Error move Pod %s: %s", podIdentifier, err)
		}
		return nil
	} else {
		return fmt.Errorf("The target service entity for move destiantion is neither a VM nor a PM.")
	}
}

// Reschdule is such an action that should be excuted in the following order:
// 1. Delete the Pod to be moved.
// 2. Replication controller will automatically create a new replica and post to api server.
// 3. At the same time create a VMTEvent, containing move action info, and post it to etcd.
// 4. Pod watcher in the vmturbo-service find the new Pod and the new VMTEvent.
// 5. Schedule Pod according to move action.
// This method delete the pod and create a VMTEvent.
func (this *Rescheduler) ReschedulePod(podIdentifier, targetNodeIdentifier string, msgID int32) error {
	podNamespace, podName, err := ProcessPodIdentifier(podIdentifier)
	if err != nil {
		return err
	}

	if targetNodeIdentifier == "" {
		return fmt.Errorf("Target node identifier should not be empty.\n")
	}

	glog.V(3).Infof("Now Moving Pod %s.", podIdentifier)

	originalPod, err := this.kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.Errorf("Error getting pod %s: %s.", podIdentifier, err)
		return err
	} else {
		glog.V(4).Infof("Successfully got pod %s.", podIdentifier)
	}

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

	// event := registry.GenerateVMTEvent(ActionMove, podNamespace, podName, targetNodeIdentifier, int(msgID))
	// glog.V(4).Infof("vmt event is %++v, with msgId %d", event, msgID)

	content := registry.NewVMTEventContentBuilder(ActionUnbind, podName, int(msgID)).
		MoveSpec(originalPod.Spec.NodeName, targetNodeIdentifier).Build()
	event := registry.NewVMTEventBuilder(podNamespace).Content(content).Create()
	glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

	// Create VMTEvent and post onto etcd.
	vmtEvents := registry.NewVMTEvents(this.kubeClient, "", this.etcdStorage)
	_, errorPost := vmtEvents.Create(&event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
		return fmt.Errorf("Error creating VMTEvents for moving Pod %s to Node %s: %s\n",
			podIdentifier, targetNodeIdentifier, errorPost)
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
