package action

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// Takes in a podIdentifier(podNamespace/podName), and extract namespace and name of the pod.
func ProcessPodIdentifier(podIdentifier string) (string, string, error) {
	// Pod identifier in vmt server passed from VMTurbo server is in "namespace/podname"
	idArray := strings.Split(podIdentifier, "/")
	if len(idArray) < 2 {
		return "", "", fmt.Errorf("Invalid Pod identifier: %s", podIdentifier)
	}
	podNamespace := idArray[0]
	podName := idArray[1]

	if podName == "" {
		return "", "", fmt.Errorf("Pod name is empty.\n")
	}
	if podNamespace == "" {
		return "", "", fmt.Errorf("Pod namespace is empty.\n")
	}
	return podNamespace, podName, nil
}

func GetPodFromCluster(kubeClient *client.Client, podIdentifier string) (*api.Pod, error) {
	podNamespace, podName, err := ProcessPodIdentifier(podIdentifier)
	if err != nil {
		return nil, err
	}

	pod, err := kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.Errorf("Error getting pod %s: %s.", podIdentifier, err)
		return nil, err
	}
	glog.V(4).Infof("Successfully got pod %s.", podIdentifier)
	return pod, nil
}

func FindReplicationControllerForPod(kubeClient *client.Client, currentPod *api.Pod) (*api.ReplicationController, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	podNamespace := currentPod.Namespace
	podName := currentPod.Name
	podLabels := currentPod.Labels

	if podLabels != nil {
		allRCs, err := GetAllReplicationControllers(kubeClient, podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, fmt.Errorf("Error  getting RC list")
		}
		rc, err := findRCBasedOnPodLabel(allRCs, podLabels)
		if err != nil {
			return nil, fmt.Errorf("Failed to find RC for Pod %s/%s: %s", podNamespace, podName, err)
		}
		return rc, nil

	} else {
		glog.Warningf("Pod %s/%s has no label. There is no RC for the Pod.", podNamespace, podName)
	}
	return nil, nil
}

// Get all replication controllers defined in the specified namespace.
func GetAllDeployments(kubeClient *client.Client, namespace string) ([]extensions.Deployment, error) {
	listOption := &api.ListOptions{
		LabelSelector: labels.Everything(),
	}
	deploymentList, err := kubeClient.Deployments(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the deployments: %s", err)
	}
	return deploymentList.Items, nil
}

func findDeploymentBasedOnPodLabel(deploymentsList []extensions.Deployment, labels map[string]string) (*extensions.Deployment, error) {
	for _, deployment := range deploymentsList {
		findDeployment := true
		// check if a Deployment controlls pods with given labels
		for key, val := range deployment.Spec.Selector.MatchLabels {
			if labels[key] == "" || labels[key] != val {
				findDeployment = false
				break
			}
		}
		if findDeployment {
			return &deployment, nil
		}
	}
	return nil, fmt.Errorf("No Deployment has selectors match Pod labels.")
}

func FindDeploymentForPod(kubeClient *client.Client, currentPod *api.Pod) (*extensions.Deployment, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	podNamespace := currentPod.Namespace
	podName := currentPod.Name
	podLabels := currentPod.Labels

	if podLabels != nil {
		allDeployments, err := GetAllDeployments(kubeClient, podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, fmt.Errorf("Error  getting Deployment list")
		}
		rc, err := findDeploymentBasedOnPodLabel(allDeployments, podLabels)
		if err != nil {
			return nil, fmt.Errorf("Failed to find Deployment for Pod %s/%s: %s", podNamespace, podName, err)
		}
		return rc, nil

	} else {
		glog.Warningf("Pod %s/%s has no label. There is no Deployment for the Pod.", podNamespace, podName)
	}
	return nil, nil
}

// Get all replication controllers defined in the specified namespace.
func GetAllReplicationControllers(kubeClient *client.Client, namespace string) ([]api.ReplicationController, error) {
	listOption := &api.ListOptions{
		LabelSelector: labels.Everything(),
	}
	rcList, err := kubeClient.ReplicationControllers(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the replication controllers: %s", err)
	}
	return rcList.Items, nil
}

func findRCBasedOnPodLabel(rcList []api.ReplicationController, labels map[string]string) (*api.ReplicationController, error) {
	for _, rc := range rcList {
		findRC := true
		// check if a RC controlls pods with given labels
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

// Get all nodes currently in K8s.
func GetAllNodes(kubeClient *client.Client) ([]api.Node, error) {
	listOption := &api.ListOptions{
		LabelSelector: labels.Everything(),
		FieldSelector: fields.Everything(),
	}
	nodeList, err := kubeClient.Nodes().List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the nodes :%s", err)
	}
	return nodeList.Items, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
// TODO. We can also create a IP->NodeName map to save time. But it consumes space.
func GetNodeNameFromIP(kubeClient *client.Client, machineIPs []string) (string, error) {
	ipAddresses := machineIPs
	// If just test locally, return the name of local cluster node.
	if localTestingFlag {
		glog.V(3).Infof("Local testing. Overrdie IP address of local node")
		ipAddresses = []string{"127.0.0.1"}
	}

	allNodes, err := GetAllNodes(kubeClient)
	if err != nil {
		return "", err
	}
	for _, node := range allNodes {
		nodeAddresses := node.Status.Addresses
		for _, nodeAddress := range nodeAddresses {
			for _, machineIP := range ipAddresses {
				if nodeAddress.Address == machineIP {
					// find node, return immediately
					return node.Name, nil
				}
			}
		}

	}
	return "", fmt.Errorf("Cannot find node with IPs %s", ipAddresses)
}

// Find which pod is the app running based on the received action request.
func FindApplicationPodProvider(kubeClient *client.Client, providers []*proto.ActionItemDTO_ProviderInfo) (*api.Pod, error) {
	if providers == nil || len(providers) < 1 {
		return nil, fmt.Errorf("Cannot find any provider.")
	}

	for _, providerInfo := range providers {
		if providerInfo == nil {
			continue
		}
		if providerInfo.GetEntityType() == proto.EntityDTO_CONTAINER_POD {
			providerIDs := providerInfo.GetIds()
			for _, id := range providerIDs {
				podProvider, err := GetPodFromIdentifier(kubeClient, id)
				if err != nil {
					glog.Errorf("Error getting pod provider from pod identifier %s", id)
					continue
				} else {
					return podProvider, nil
				}

			}
		}
	}
	return nil, fmt.Errorf("Cannot find any Pod provider")
}

// Check is an ID is a pod identifier.
func GetPodFromIdentifier(kubeClient *client.Client, id string) (*api.Pod, error) {

	podNamespace, podName, err := ProcessPodIdentifier(id)
	if err != nil {
		return nil, err
	}

	pod, err := kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		return nil, err
	} else if pod.Name != podName || pod.Namespace != podNamespace {
		// Not sure is this is necessary.
		return nil, fmt.Errorf("Got wrong pod, desired: %s/%s, found: %s/%s", podNamespace, podName, pod.Namespace, pod.Name)
	}
	return pod, nil

}
