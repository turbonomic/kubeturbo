package util

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// Find RC based on pod labels.
// TODO. change this. Find rc based on its name and namespace or rc's UID.
func FindReplicationControllerForPod(kubeClient *client.Client, currentPod *api.Pod) (*api.ReplicationController, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	podNamespace := currentPod.Namespace
	podName := currentPod.Name
	podLabels := currentPod.Labels

	if podLabels != nil {
		allRCs, err := GetAllReplicationControllers(kubeClient, podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, errors.New("Error  getting RC list")
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

// TODO. change this. Find deployment based on its name and namespace or UID.
func findDeploymentBasedOnPodLabel(deploymentsList []extensions.Deployment, labels map[string]string) (*extensions.Deployment, error) {
	for _, deployment := range deploymentsList {
		findDeployment := true
		// check if a Deployment controls pods with given labels
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
	return nil, errors.New("No Deployment has selectors match Pod labels.")
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
			return nil, errors.New("Error  getting Deployment list")
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
	return nil, errors.New("No RC has selectors match Pod labels.")
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
		return nil, errors.New("Cannot find any provider.")
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
	return nil, errors.New("Cannot find any Pod provider")
}

// Get Pod from a turbo pod UUID.
func GetPodFromIdentifier(kubeClient *client.Client, id string) (*api.Pod, error) {
	podUID, podNamespace, podName, err := probe.BreakdownTurboPodUUID(id)
	if err != nil {
		return nil, fmt.Errorf("Failed to breakdown turbo pod UUID: %s", err)
	}

	pod, err := kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		return nil, err
	} else if string(pod.UID) != podUID || pod.Name != podName || pod.Namespace != podNamespace {
		return nil, fmt.Errorf("Got wrong pod, wanted: %s/%s with UID %s, "+
			"found: %s/%s with UID %s", podNamespace, podName, podUID, pod.Namespace, pod.Name, pod.UID)
	}
	glog.V(4).Infof("Successfully got pod %s/%s.", podNamespace, podName)
	return pod, nil
}

// Given namespace and name, return an identifier in the format, namespace/name
func BuildIdentifier(namespace, name string) string {
	return namespace + "/" + name
}
