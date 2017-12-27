package util

import (
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

var (
	listOption = metav1.ListOptions{}
)

// Find RC based on pod labels.
// TODO. change this. Find rc based on its name and namespace or rc's UID.
func FindReplicationControllerForPod(kubeClient *client.Clientset, currentPod *api.Pod) (*api.ReplicationController, error) {
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
func GetAllDeployments(kubeClient *client.Clientset, namespace string) ([]v1beta1.Deployment, error) {
	deploymentList, err := kubeClient.AppsV1beta1().Deployments(namespace).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the deployments: %s", err)
	}
	return deploymentList.Items, nil
}

// TODO. change this. Find deployment based on its name and namespace or UID.
func findDeploymentBasedOnPodLabel(deploymentsList []v1beta1.Deployment, labels map[string]string) (*v1beta1.Deployment, error) {
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

func FindDeploymentForPod(kubeClient *client.Clientset, currentPod *api.Pod) (*v1beta1.Deployment, error) {
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
func GetAllReplicationControllers(kubeClient *client.Clientset, namespace string) ([]api.ReplicationController, error) {
	rcList, err := kubeClient.CoreV1().ReplicationControllers(namespace).List(listOption)
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
func GetAllNodes(kubeClient *client.Clientset) ([]api.Node, error) {
	nodeList, err := kubeClient.CoreV1().Nodes().List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the nodes :%s", err)
	}
	return nodeList.Items, nil
}

// try to get k8s.node by nodeName
func GetNodebyName(kubeClient *client.Clientset, nodeName string) (*api.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get node(%v): %v", nodeName, err)
		return nil, err
	}

	return node, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
func GetNodebyUUID(kubeClient *client.Clientset, uuid string) (*api.Node, error) {
	allNodes, err := GetAllNodes(kubeClient)
	if err != nil {
		glog.Errorf("Failed to list nodes: %v", err)
		return nil, err
	}

	for i := range allNodes {
		node := &allNodes[i]
		if strings.EqualFold(uuid, node.Status.NodeInfo.SystemUUID) {
			return node, nil
		}
	}

	return nil, fmt.Errorf("Cannot find node with UUID %s", uuid)
}

func GetNodeFromProperties(kubeClient *client.Clientset, properties []*proto.EntityDTO_EntityProperty) (*api.Node, error) {
	name := property.GetNodeNameFromProperty(properties)
	if len(name) < 1 {
		err := fmt.Errorf("cannot found node name from properites: %++v", properties)
		glog.Error(err)
		return nil, err
	}

	return GetNodebyName(kubeClient, name)
}

// Iterate all nodes to find the name of the node which has the provided IP address.
// TODO. We can also create a IP->NodeName map to save time. But it consumes space.
func GetNodeNameFromIP(kubeClient *client.Clientset, machineIPs []string) (string, error) {
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

// Get a pod based on received entity properties.
func GetPodFromProperties(kubeClient *client.Clientset, entityType proto.EntityDTO_EntityType,
	properties []*proto.EntityDTO_EntityProperty) (*api.Pod, error) {
	var podNamespace, podName string
	switch entityType {
	case proto.EntityDTO_APPLICATION:
		podNamespace, podName = property.GetApplicationHostingPodInfoFromProperty(properties)
	case proto.EntityDTO_CONTAINER_POD:
		podNamespace, podName = property.GetPodInfoFromProperty(properties)
	default:
		return nil, fmt.Errorf("cannot find pod based on properties of an entity with type: %s", entityType)
	}
	if podNamespace == "" || podName == "" {
		return nil, fmt.Errorf("railed to find  pod info from pod properties: %v", properties)
	}
	return kubeClient.CoreV1().Pods(podNamespace).Get(podName, metav1.GetOptions{})
}

// Get a pod instance from the uuid of a pod. Since there is no support for uuid lookup, we have to get all the pods
// and then find the correct pod based on uuid match.
func GetPodFromUUID(kubeClient *client.Clientset, podUUID string) (*api.Pod, error) {
	namespace := api.NamespaceAll
	podList, err := kubeClient.CoreV1().Pods(namespace).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("error getting all the desired pods from Kubernetes cluster: %s", err)
	}
	for _, pod := range podList.Items {
		if string(pod.UID) == podUUID {
			return &pod, nil
		}
	}
	return nil, fmt.Errorf("cannot find pod based on given uuid: %s", podUUID)
}

// Find which pod is the app running based on the received action request.
func FindApplicationPodProvider(kubeClient *client.Clientset, providers []*proto.ActionItemDTO_ProviderInfo) (*api.Pod, error) {
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
				podProvider, err := GetPodFromUUID(kubeClient, id)
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

// Given namespace and name, return an identifier in the format, namespace/name
func BuildIdentifier(namespace, name string) string {
	return namespace + "/" + name
}
