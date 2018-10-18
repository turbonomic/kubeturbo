package util

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"strings"
)

const (
	osSccAnnotation = "openshift.io/scc"
	osSccAllowAll   = "*"
)

var (
	listOption = metav1.ListOptions{}
)

// Get all nodes currently in K8s.
func GetAllNodes(kubeClient *client.Clientset) ([]api.Node, error) {
	nodeList, err := kubeClient.CoreV1().Nodes().List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the nodes :%s", err)
	}
	return nodeList.Items, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
// TODO. We can also create a IP->NodeName map to save time. But it consumes space.
func GetNodebyIP(kubeClient *client.Clientset, machineIPs []string) (*api.Node, error) {
	ipAddresses := machineIPs
	allNodes, err := GetAllNodes(kubeClient)
	if err != nil {
		return nil, err
	}
	for i := range allNodes {
		node := &allNodes[i]
		nodeAddresses := node.Status.Addresses
		for _, nodeAddress := range nodeAddresses {
			for _, machineIP := range ipAddresses {
				if nodeAddress.Address == machineIP {
					return node, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("Cannot find node with IPs %s", ipAddresses)
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

// Given namespace and name, return an identifier in the format, namespace/name
func BuildIdentifier(namespace, name string) string {
	return namespace + "/" + name
}

// check whether parentKind is supported for MovePod/ResizeContainer actions
// currently, these actions can only works on barePod, ReplicaSet, and ReplicationController
//   Note: pod's parent cannot be Deployment. Deployment will create/control ReplicaSet, and ReplicaSet will create/control Pods.
func SupportedParent(parentKind string) bool {
	if parentKind == "" {
		return true
	}

	if strings.EqualFold(parentKind, util.KindReplicaSet) {
		return true
	}

	if strings.EqualFold(parentKind, util.KindReplicationController) {
		return true
	}

	return false
}

func AddAnnotation(pod *api.Pod, key, value string) {
	annotations := pod.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
		pod.Annotations = annotations
	}

	if oldv, ok := annotations[key]; ok {
		glog.Warningf("Overwrite pod(%v/%v)'s annotation (%v=%v)", pod.Namespace, pod.Name, key, oldv)
	}
	annotations[key] = value
	return
}

// SupportPrivilegePod checks the pod annotations and returns true if the priviledge requirement
// is supported. Currently, it only matters for Openshift clusters as it checks the annotation of
// Openshift SCC (openshift.io/scc). Kubeturbo supports the restricted (default) SCC and rejects
// other non-empty SCC's if the annotation is set.
// See details on https://docs.openshift.org/latest/admin_guide/manage_scc.html.
func SupportPrivilegePod(pod *api.Pod, sccAllowedSet map[string]struct{}) bool {
	if pod == nil || pod.Annotations == nil || pod.Annotations[osSccAnnotation] == "" {
		return true
	}

	if _, ok := sccAllowedSet[osSccAllowAll]; ok {
		return true
	}

	podScc := pod.Annotations[osSccAnnotation]
	if _, ok := sccAllowedSet[podScc]; !ok {
		glog.Warningf("Target pod %s has unsupported SCC %s. Please add the SCC to "+
			"--scc-support argument in kubeturbo yaml to allow execution.", pod.Name, podScc)
		return false
	}

	return true
}
