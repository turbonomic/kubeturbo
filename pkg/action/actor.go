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
		} else if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_VIRTUAL_APPLICATION {
			return this.UnBind(actionItem, msgID)
		} else {
			return fmt.Errorf("The service entity to be moved is not a Pod. Got %s", actionItem.GetTargetSE().GetEntityType())
		}

	} else if actionItem.GetActionType() == sdk.ActionItemDTO_PROVISION {
		glog.V(4).Infof("Now Provision Pods")
		targetEntityType := actionItem.GetTargetSE().GetEntityType()
		if targetEntityType == sdk.EntityDTO_CONTAINER_POD ||
			targetEntityType == sdk.EntityDTO_APPLICATION {

			var podIdentifier string
			if targetEntityType == sdk.EntityDTO_CONTAINER_POD {
				targetPod := actionItem.GetTargetSE()
				podIdentifier = targetPod.GetId()
			} else if targetEntityType == sdk.EntityDTO_APPLICATION {
				foundPodId, err := this.findApplicationPodProvider(actionItem)
				if err != nil {
					return err
				}
				podIdentifier = foundPodId
			}

			// find related replication controller through identifier.
			podIds := strings.Split(string(podIdentifier), "/")
			if len(podIds) != 2 {
				return fmt.Errorf("Not a valid pod identifier: %s", podIdentifier)
			}

			podNamespace := podIds[0]

			podName := podIds[1]
			pod, err := this.KubeClient.Pods(podNamespace).Get(podName)
			if err != nil {
				glog.Errorf("Error getting the pod %s: %s", podIdentifier, err)
			}
			generateName := pod.GenerateName
			// TODO, pod created by RC should have the generateName
			if generateName == "" || len(generateName) < 2 {
				return fmt.Errorf("Pod %s is not created by RC. Cannot provision", podIdentifier)
			}

			// delete the tailing '-'
			replicationControllerName := generateName[0 : len(generateName)-1]
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

func (this *KubernetesActionExecutor) findRC(podName, podNamespace string) (*api.ReplicationController, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	currentPod, err := this.KubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.Errorf("Error getting pod name %s: %s.\n", podName, err)
		return nil, err
	}
	podLabels := currentPod.Labels
	if podLabels != nil {
		currentRCs, err := this.GetAllRC(podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, fmt.Errorf("Error  getting RC list")
		}
		rc, err := findRCBasedOnLabel(currentRCs, podLabels)
		if err != nil {
			return nil, fmt.Errorf("Failed to find RC for Pod %s/%s: %s", podNamespace, podName, err)
		}
		return rc, nil

	} else {
		glog.Warningf("Pod %s/%s has no label. There is no RC for the Pod.", podNamespace, podName)
	}
	return nil, nil
}

func findRCBasedOnLabel(rcList []api.ReplicationController, labels map[string]string) (*api.ReplicationController, error) {
	for _, rc := range rcList {
		findRC := true
		// use function to check if a given RC will take care of this pod
		for key, val := range rc.Spec.Selector {
			if labels[key] == "" || labels[key] != val {
				findRC = false
				break
			}
		}
		if findRC {
			return &rc, nil
		}
	}
	return nil, fmt.Errorf("No RC has selectors match Pod labels.")
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

func (this *KubernetesActionExecutor) UnBind(actionItem *sdk.ActionItemDTO, msgID int32) error {
	// TODO, currently UNBIND action is sent in as MOVE. Need to change in the future.
	if currentSE := actionItem.GetCurrentSE(); currentSE.GetEntityType() == sdk.EntityDTO_APPLICATION {
		// TODO find the pod name based on application ID. App id is in the following format.
		// !!
		// ProcessName::PodNamespace/PodName
		// NOT GOOD. Will change Later!
		appName := currentSE.GetId()
		ids := strings.Split(appName, "::")
		if len(ids) < 2 {
			return fmt.Errorf("%s is not a valid Application ID. Unbind failed.", appName)
		}

		podIdentifier := ids[1]
		// Pod identifier in vmt server passed from VMTurbo server is in "namespace/podname"
		idArray := strings.Split(string(podIdentifier), "/")
		if len(idArray) < 2 {
			return fmt.Errorf("Invalid Pod identifier: %s", podIdentifier)
		}
		podNamespace := idArray[0]
		podName := idArray[1]

		targetReplicationController, err := this.findRC(podName, podNamespace)
		if err != nil {
			return err
		}
		currentReplica := targetReplicationController.Spec.Replicas
		if currentReplica < 1 {
			return fmt.Errorf("Replica of %s is already 0. Cannot scale in anymore.", targetReplicationController.Name)
		}
		err = this.updateReplicasValue(*targetReplicationController, currentReplica-1)
		action := "unbind"
		vmtEvents := registry.NewVMTEvents(this.KubeClient, "", this.EtcdStorage)
		event := registry.GenerateVMTEvent(action, podNamespace, targetReplicationController.Name, "not specified", int(msgID))
		glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
		_, errorPost := vmtEvents.Create(event)
		if errorPost != nil {
			return fmt.Errorf("Error posting vmtevent: %s\n", errorPost)
		}
		return nil
	}
	return fmt.Errorf("Wrong type for CurrentSE in AcitonItemDTO")
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
	err = this.updateReplicasValue(targetReplicationController, newReplicas)
	if err != nil {
		return err
	}
	action := "provision"
	vmtEvents := registry.NewVMTEvents(this.KubeClient, "", this.EtcdStorage)
	event := registry.GenerateVMTEvent(action, targetReplicationController.Namespace, targetReplicationController.Name, "not specified", int(msgID))
	glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}
	return
}

func (this *KubernetesActionExecutor) updateReplicasValue(targetReplicationController api.ReplicationController, newReplicas int) error {
	targetReplicationController.Spec.Replicas = newReplicas
	namespace := targetReplicationController.Namespace
	KubeClient := this.KubeClient
	newRC, err := KubeClient.ReplicationControllers(namespace).Update(&targetReplicationController)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s: %s", targetReplicationController.Name, err)
	}
	glog.V(4).Infof("New replicas of %s is %d", newRC.Name, newRC.Spec.Replicas)
	return nil
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

// Find which pod is the app running based on the received action request.
func (this *KubernetesActionExecutor) findApplicationPodProvider(action *sdk.ActionItemDTO) (string, error) {
	providers := action.GetProviders()
	if providers == nil || len(providers) < 1 {
		return "", fmt.Errorf("Don't find provider in actionItemDTO for provision Application %s", action.GetTargetSE().GetId())
	}

	for _, providerInfo := range providers {
		if providerInfo == nil {
			continue
		}
		if providerInfo.GetEntityType() == sdk.EntityDTO_CONTAINER_POD {
			providerIDs := providerInfo.GetIds()
			for _, id := range providerIDs {
				isTrue, err := this.isPodIdentifier(id)
				if err != nil {
					return "", err
				}
				if isTrue {
					return id, nil
				}
			}
		}
	}

	return "", fmt.Errorf("Cannot find Pod Provider for for provision Application %s", action.GetTargetSE().GetId())
}

// Check is an ID is a pod identifier.
func (this *KubernetesActionExecutor) isPodIdentifier(id string) (bool, error) {
	// A valid podIdentifier is defined as foo/bar
	podIds := strings.Split(id, "/")
	if len(podIds) != 2 {
		return false, nil
	}

	podNamespace := podIds[0]
	podName := podIds[1]
	pod, err := this.KubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		return false, err
	} else if pod.Name != podName && pod.Namespace != podNamespace {
		return false, fmt.Errorf("Got wrong pod, want to find %s/%s, but found %s/%s", podNamespace, podName, pod.Namespace, pod.Name)
	}
	return true, nil

}
