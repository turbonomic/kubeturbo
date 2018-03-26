package util

import (
	"encoding/json"
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
	osSccAnnotation          = "openshift.io/scc"
	supportedOsSccAnnotation = "restricted"
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

func GetPod(kubeClient *client.Clientset, namespace, name string) (*api.Pod, error) {
	return kubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
}

func parseOwnerReferences(owners []metav1.OwnerReference) (string, string) {
	for i := range owners {
		owner := &owners[i]
		if *(owner.Controller) && len(owner.Kind) > 0 && len(owner.Name) > 0 {
			return owner.Kind, owner.Name
		}
	}

	return "", ""
}

func GetPodParentInfo(pod *api.Pod) (string, string, error) {
	//1. check ownerReferences:

	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		kind, name := parseOwnerReferences(pod.OwnerReferences)
		if len(kind) > 0 && len(name) > 0 {
			return kind, name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in OwnerReferences.", pod.Namespace, pod.Name)

	//2. check annotations:
	if pod.Annotations != nil && len(pod.Annotations) > 0 {
		key := "kubernetes.io/created-by"
		if value, ok := pod.Annotations[key]; ok {

			var ref api.SerializedReference

			if err := json.Unmarshal([]byte(value), &ref); err != nil {
				err = fmt.Errorf("failed to decode parent annoation:%v", err)
				glog.Errorf("%v\n%v", err, value)
				return "", "", err
			}

			return ref.Reference.Kind, ref.Reference.Name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in Annotations.", pod.Namespace, pod.Name)

	return "", "", nil
}

// get grandParent(parent's parent) information of a pod: kind, name
// If parent does not have parent, then return parent info.
// Note: if parent kind is "ReplicaSet", then its parent's parent can be a "Deployment"
func GetPodGrandInfo(kclient *client.Clientset, pod *api.Pod) (string, string, error) {
	//1. get Parent info: kind and name;
	kind, name, err := GetPodParentInfo(pod)
	if err != nil {
		return "", "", err
	}

	//2. if parent is "ReplicaSet", check parent's parent
	if strings.EqualFold(kind, "ReplicaSet") {
		//2.1 get parent object
		rs, err := kclient.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			err = fmt.Errorf("Failed to get ReplicaSet[%v/%v]: %v", pod.Namespace, name, err)
			glog.Error(err.Error())
			return "", "", err
		}

		//2.2 get parent's parent info by parsing ownerReferences:
		// TODO: The ownerReferences of ReplicaSet is supported only in 1.6.0 and afetr
		if rs.OwnerReferences != nil && len(rs.OwnerReferences) > 0 {
			gkind, gname := parseOwnerReferences(rs.OwnerReferences)
			if len(gkind) > 0 && len(gname) > 0 {
				return gkind, gname, nil
			}
		}
	}

	return kind, name, nil
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
func SupportPrivilegePod(pod *api.Pod) bool {
	annotations := pod.Annotations
	key := osSccAnnotation
	value := supportedOsSccAnnotation
	if annotations != nil && annotations[key] != "" && annotations[key] != value {
		glog.Warningf("The pod %s has unsupported privilege %s", pod.Name, annotations[key])
		return false
	}

	return true
}
