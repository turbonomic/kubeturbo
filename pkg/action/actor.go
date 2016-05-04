package action

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

// KubernetesActionExecutor is responsilbe for executing different kinds of actions requested by vmt server.
type KubernetesActionExecutor struct {
	KubeClient  *client.Client
	EtcdStorage storage.Storage
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewKubeActor(client *client.Client, etcdStorage storage.Storage) *KubernetesActionExecutor {
	return &KubernetesActionExecutor{
		KubeClient:  client,
		EtcdStorage: etcdStorage,
	}
}

// Switch between different types of the actions. Then call the actually corresponding execution method.
func (this *KubernetesActionExecutor) ExcuteAction(actionItem *sdk.ActionItemDTO, msgID int32) error {
	if actionItem == nil {
		return fmt.Errorf("ActionItem received in is null")
	}
	glog.V(3).Infof("Receive a %s action request.", actionItem.GetActionType())

	if actionItem.GetActionType() == sdk.ActionItemDTO_MOVE {
		glog.V(4).Infof("Now moving pod")
		// Here we must make sure the TargetSE is a Pod and NewSE is either a VirtualMachine or a PhysicalMachine.
		if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_CONTAINER_POD {
			newSEType := actionItem.GetNewSE().GetEntityType()
			if newSEType == sdk.EntityDTO_VIRTUAL_MACHINE || newSEType == sdk.EntityDTO_PHYSICAL_MACHINE {
				targetNode := actionItem.GetNewSE()

				var machineIPs []string

				switch newSEType {
				case sdk.EntityDTO_VIRTUAL_MACHINE:
					// K8s uses Ip address as the Identifier. The VM name passed by actionItem is the display name
					// that dscovered by hypervisor. So here we must get the ip address from virtualMachineData in
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

				glog.V(3).Infof("The IP of targetNode is %v", machineIPs)
				if machineIPs == nil {
					return fmt.Errorf("Miss IP addresses in ActionItemDTO.")
				}

				// Get the actual node name from Kubernetes cluster based on IP address.
				nodeIdentifier, err := this.getNodeNameFromIP(machineIPs)
				if err != nil {
					return err
				}

				targetPod := actionItem.GetTargetSE()
				podIdentifier := targetPod.GetId()

				err = this.MovePod(podIdentifier, nodeIdentifier, msgID)
				if err != nil {
					return fmt.Errorf("Error move Pod %s: %s", podIdentifier, err)
				}
			} else {
				return fmt.Errorf("The target service entity for move destiantion is neither a VM nor a PM.")
			}
		} else {
			return fmt.Errorf("The service entity to be moved is not a Pod")
		}

	} else if actionItem.GetActionType() == sdk.ActionItemDTO_PROVISION {
		glog.V(4).Infof("Now Provision Pods")
		if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_CONTAINER_POD {
			targetPod := actionItem.GetTargetSE()
			podIdentifier := targetPod.GetId()

			// find related replication controller through identifier.
			podIds := strings.Split(string(podIdentifier), "/")
			if len(podIds) != 2 {
				return fmt.Errorf("Not a valid pod identifier: %s", podIdentifier)
			}

			podNamespace := podIds[0]
			podNames := strings.Split(string(podIds[1]), "-")
			if len(podNames) != 2 {
				return fmt.Errorf("Cannot parse pod with name: %s", podIds[1])
			}
			replicationControllerName := podNames[0]
			targetReplicationController, err := this.getReplicationController(replicationControllerName, podNamespace)
			if err != nil {
				return fmt.Errorf("Error getting replication controller related to pod %s: %s", podIdentifier, err)
			}
			if &targetReplicationController == nil {
				return fmt.Errorf("No replication controller defined with pod %s", podIdentifier)
			}
			currentReplica := targetReplicationController.Spec.Replicas
			err = this.ProvisionPods(targetReplicationController, currentReplica+1, msgID)
			if err != nil {
				return fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}
		}
	} else {
		return fmt.Errorf("Action %s not supported", actionItem.GetActionType())
	}
	return nil
}

// Move is such an action that should be excuted in the following order:
// 1. Delete the Pod to be moved.
// 2. Replication controller will automatically create a new replica and post to api server.
// 3. At the same time create a VMTEvent, containing move action info, and post it to etcd.
// 4. Pod watcher in the vmturbo-service find the new Pod and the new VMTEvent.
// 5. Schedule Pod according to move action.
// This method delete the pod and create a VMTEvent.
func (this *KubernetesActionExecutor) MovePod(podIdentifier, targetNodeIdentifier string, msgID int32) error {
	// Pod identifier in vmt server passed from VMTurbo server is in "namespace/podname"
	idArray := strings.Split(string(podIdentifier), "/")
	if len(idArray) < 2 {
		return fmt.Errorf("Invalid Pod identifier: %s", podIdentifier)
	}
	podNamespace := idArray[0]
	podName := idArray[1]

	if podName == "" {
		return fmt.Errorf("Pod name should not be empty.\n")
	}
	if podNamespace == "" {
		return fmt.Errorf("Pod namespace should not be empty.\n")
	}
	if targetNodeIdentifier == "" {
		return fmt.Errorf("Target node identifier should not be empty.\n")
	}

	glog.V(3).Infof("Now Moving Pod %s in namespace %s.", podName, podNamespace)

	hasRC, err := this.isCreatedByRC(podName, podNamespace)
	if err != nil {
		glog.Errorf("Error creating Pod for move: %s\n", err)
	}
	var currentPod *api.Pod
	if !hasRC {
		currentPod, err = this.KubeClient.Pods(podNamespace).Get(podName)
		if err != nil {
			glog.Errorf("Error getting pod %s: %s.", podName, err)
			return err
		} else {
			glog.V(4).Infof("Successfully got pod %s.", podName)
		}
	}

	// Delete pod
	err = this.KubeClient.Pods(podNamespace).Delete(podName, nil)
	if err != nil {
		glog.Errorf("Error deleting pod %s: %s.\n", podName, err)
		return fmt.Errorf("Error deleting pod %s: %s.\n", podName, err)
	} else {
		glog.V(3).Infof("Successfully delete pod %s.\n", podName)
	}

	action := "move"

	// Create VMTEvent and post onto etcd.
	vmtEvents := registry.NewVMTEvents(this.KubeClient, "", this.EtcdStorage)
	event := registry.GenerateVMTEvent(action, podNamespace, podName, targetNodeIdentifier, int(msgID))
	glog.V(4).Infof("vmt event is %++v, with msgId %d", event, msgID)
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
		fmt.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	if !hasRC {
		time.Sleep(time.Second * 3)
		_, err := this.KubeClient.Pods(podNamespace).Get(podName)
		if err != nil {
			glog.V(3).Infof("Get deleted standalone Pos with error %s. This is expected.", err)
		}

		pod := copyPod(currentPod)
		_, err = this.KubeClient.Pods(podNamespace).Create(pod)
		if err != nil {
			glog.Errorf("Error creating pod: %s", err)
			return err
		}
	}
	return nil
}

func (this *KubernetesActionExecutor) isCreatedByRC(podName, podNamespace string) (bool, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	hasRC := false
	currentPod, err := this.KubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.Errorf("Error getting pod name %s: %s.\n", podName, err)
		return hasRC, err
	}
	podLabels := currentPod.Labels
	if podLabels != nil {
		currentRCs, err := this.GetAllRC(podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return hasRC, fmt.Errorf("Error  getting RC list")
		}
		rcList := currentRCs
		hasRC = checkRCList(rcList, podLabels)
		if hasRC {
			return true, nil
		}
	}
	glog.V(4).Infof("HasRC is %v", hasRC)
	return hasRC, nil
}

func checkRCList(rcList []api.ReplicationController, labels map[string]string) bool {
	for _, rc := range rcList {
		// use function to check if a given RC will take care of this pod
		for key, val := range rc.Spec.Selector {
			if labels[key] == val {
				return true
			}
		}
	}
	return false
}

func copyPod(podToCopy *api.Pod) *api.Pod {
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

// This is used to scale up and down. So we need the pod namespace and label here.
func (this *KubernetesActionExecutor) UpdateReplicas(podLabel, namespace string, newReplicas int) (err error) {
	targetRC, err := this.getReplicationController(podLabel, namespace)
	if err != nil {
		return fmt.Errorf("Error getting replication controller: %s", err)
	}
	if &targetRC == nil {
		// TODO. Not sure here need error or just a warning.
		glog.Warning("This pod is not managed by any replication controllers")
		return fmt.Errorf("This pod is not managed by any replication controllers")
	}
	err = this.ProvisionPods(targetRC, newReplicas, -1)
	return err
}

// Update replica of the target replication controller.
func (this *KubernetesActionExecutor) ProvisionPods(targetReplicationController api.ReplicationController, newReplicas int, msgID int32) (err error) {
	targetReplicationController.Spec.Replicas = newReplicas
	namespace := targetReplicationController.Namespace
	KubeClient := this.KubeClient
	newRC, err := KubeClient.ReplicationControllers(namespace).Update(&targetReplicationController)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s: %s", targetReplicationController.Name, err)
	}
	glog.V(4).Infof("New replicas of %s is %d", newRC.Name, newRC.Spec.Replicas)

	action := "provision"
	vmtEvents := registry.NewVMTEvents(this.KubeClient, "", this.EtcdStorage)
	event := registry.GenerateVMTEvent(action, namespace, newRC.Name, "not specified", int(msgID))
	glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}
	return
}

// Get the replication controller instance according to the name and namespace.
func (this *KubernetesActionExecutor) getReplicationController(rcName, namespace string) (api.ReplicationController, error) {
	var targetRC api.ReplicationController

	replicationControllers, err := this.GetAllRC(namespace)
	if err != nil {
		return targetRC, err
	}
	if len(replicationControllers) < 1 {
		return targetRC, fmt.Errorf("There is no replication controller defined in current cluster")
	}
	for _, rc := range replicationControllers {
		if rcName == rc.Name {
			targetRC = rc
		}
	}
	return targetRC, nil
}

// Get all replication controllers defined in the specified namespace.
func (this *KubernetesActionExecutor) GetAllRC(namespace string) (replicationControllers []api.ReplicationController, err error) {
	listOption := &api.ListOptions{
		LabelSelector: labels.Everything(),
	}
	rcList, err := this.KubeClient.ReplicationControllers(namespace).List(*listOption)
	if err != nil {
		glog.Errorf("Error when getting all the replication controllers: %s", err)
	}
	replicationControllers = rcList.Items
	for _, rc := range replicationControllers {
		glog.V(4).Infof("Find replication controller: %s", rc.Name)
	}
	return
}

// Get all nodes currently in K8s.
func (this *KubernetesActionExecutor) GetAllNodes() ([]*api.Node, error) {
	listOption := &api.ListOptions{
		LabelSelector: labels.Everything(),
		FieldSelector: fields.Everything(),
	}
	nodeList, err := this.KubeClient.Nodes().List(*listOption)
	if err != nil {
		return nil, err
	}
	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		nodeItems = append(nodeItems, &node)
	}
	return nodeItems, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
// TODO. We can also create a IP->NodeName map to save time. But it consumes space.
func (this *KubernetesActionExecutor) getNodeNameFromIP(machineIPs []string) (string, error) {
	allNodes, err := this.GetAllNodes()
	if err != nil {
		return "", fmt.Errorf("Error listing all availabe nodes in Kubernetes: %s", err)
	}
	for _, node := range allNodes {
		nodeAddresses := node.Status.Addresses
		for _, nodeAddress := range nodeAddresses {
			for _, machineIP := range machineIPs {
				if nodeAddress.Address == machineIP {
					return node.Name, nil
				}
			}
		}

	}

	// If just test locally, return the name of local cluster node.
	if localTestingFlag {
		glog.V(3).Infof("Local testing. Didn't find node with IPs %s, will return name of local node", machineIPs)
		localAddress := []string{"127.0.0.1"}
		return this.getNodeNameFromIP(localAddress)
	}

	return "", fmt.Errorf("Cannot find node with IPs %s", machineIPs)
}
