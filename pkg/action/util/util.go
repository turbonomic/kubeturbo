package util

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	discoveryutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	osSccAnnotation = "openshift.io/scc"
	osSccAllowAll   = "*"
)

var listOption = metav1.ListOptions{}

// Get all nodes currently in K8s.
func GetAllNodes(kubeClient *client.Clientset) ([]api.Node, error) {
	nodeList, err := kubeClient.CoreV1().Nodes().List(context.TODO(), listOption)
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
	glog.Warningf("Could not get node by node IP(%v): %v", strings.Join(ipAddresses, "/"), err)
	return nil, fmt.Errorf("cannot find node by IPs %s", strings.Join(ipAddresses, "/"))
}

// try to get k8s.node by nodeName
func GetNodebyName(kubeClient *client.Clientset, nodeName string) (*api.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Warningf("Could not get node by node name(%v): %v", nodeName, err)
		return nil, fmt.Errorf("cannot find node by node name %s", nodeName)
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

	glog.Warningf("Could not get node by node UUID(%v): %v", uuid, err)
	return nil, fmt.Errorf("cannot find node by UUID %s", uuid)
}

func GetNodeFromProperties(kubeClient *client.Clientset, properties []*proto.EntityDTO_EntityProperty) (*api.Node, error) {
	name, err := property.GetNodeNameFromProperties(properties)
	if err != nil {
		glog.Warningln("Could not get node by node properties")
		if glog.V(3) {
			glog.Infof("The node properities are %++v", properties)
			glog.Infof("%v", err)
		}
		return nil, fmt.Errorf("cannot find node by node properites")
	}

	return GetNodebyName(kubeClient, name)
}

// Given namespace and name, return an identifier in the format, namespace/name
func BuildIdentifier(namespace, name string) string {
	return namespace + "/" + name
}

// check whether parentKind is supported for MovePod/ResizeContainer actions
// currently, these actions can only works on barePod, ReplicaSet, and ReplicationController
// and on Daemonsets if the action is resize.
//
//	Note: pod's parent cannot be Deployment. Deployment will create/control ReplicaSet, and ReplicaSet will create/control Pods.
func SupportedParent(ownerInfo discoveryutil.OwnerInfo, isResize bool) bool {
	if discoveryutil.IsOwnerInfoEmpty(ownerInfo) {
		return true
	}
	if isResize && strings.EqualFold(ownerInfo.Kind, util.KindDaemonSet) {
		return true
	}
	if strings.EqualFold(ownerInfo.Kind, util.KindReplicaSet) {
		return true
	}
	if strings.EqualFold(ownerInfo.Kind, util.KindReplicationController) {
		return true
	}
	if strings.EqualFold(ownerInfo.Kind, util.KindVirtualMachineInstance) {
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

func AddLabel(pod *api.Pod, key, value string) {
	labels := pod.Labels
	if labels == nil {
		labels = make(map[string]string)
		pod.Labels = labels
	}

	if oldv, ok := labels[key]; ok {
		glog.Warningf("Overwrite pod(%v/%v)'s labels (%v=%v)", pod.Namespace, pod.Name, key, oldv)
	}
	labels[key] = value
	return
}

// Sanitizes the resources requests & limits of a Pod spec.
// A previous bug could introduce new requests/limits with zero values when not present in the spec.
// This can cause problems in namespaces with LimitRanges, resulting in move and resize action failures
// due to the value zero being outside the allowed range as defined on the LimitRange. For more
// information, see bug: https://jsw.ibm.com/browse/TRB-54994
func SanitizeResources(podSpec *api.PodSpec) {
	for i := range podSpec.Containers {
		container := &podSpec.Containers[i]
		containerRequests := container.Resources.Requests
		containerLimits := container.Resources.Limits

		if len(containerRequests) > 0 {
			if containerRequests.Cpu().IsZero() {
				glog.V(4).Infof("CPU requests are 0 for Container %s. Removing CPU requests from spec.", container.Name)
				delete(containerRequests, api.ResourceCPU)
			}
			if containerRequests.Memory().IsZero() {
				glog.V(4).Infof("Memory requests are 0 for Container %s. Removing Memory requests from spec.", container.Name)
				delete(containerRequests, api.ResourceMemory)
			}
		}

		if len(containerLimits) > 0 {
			if containerLimits.Cpu().IsZero() {
				glog.V(4).Infof("CPU limit is 0 for Container %s. Removing CPU limit from spec.", container.Name)
				delete(containerLimits, api.ResourceCPU)
			}
			if containerLimits.Memory().IsZero() {
				glog.V(4).Infof("Memory limit i 0 for Container %s. Removing Memory limit from spec.", container.Name)
				delete(containerLimits, api.ResourceMemory)
			}
		}
	}
}

// SupportPrivilegePod checks the pod annotations and returns true if the priviledge requirement
// is supported. Currently, it only matters for Openshift clusters as it checks the annotation of
// Openshift SCC (openshift.io/scc). Kubeturbo supports the restricted (default) SCC and rejects
// other non-empty SCC's if the annotation is set.
// See details on https://docs.openshift.org/latest/admin_guide/manage_scc.html.
func SupportPrivilegePod(pod *api.Pod, sccAllowedSet map[string]struct{}) (bool, error) {
	if pod == nil || pod.Annotations == nil || pod.Annotations[osSccAnnotation] == "" {
		return true, nil
	}

	if _, ok := sccAllowedSet[osSccAllowAll]; ok {
		return true, nil
	}

	podScc := pod.Annotations[osSccAnnotation]
	if _, ok := sccAllowedSet[podScc]; !ok {
		return false, fmt.Errorf("pod %s/%s has unsupported SCC %s. Please add the SCC level to "+
			"--scc-support cmd line argument in kubeturbo deployment yaml to allow execution", pod.Namespace, pod.Name, podScc)
	}

	return true, nil
}

func GetActionItemId(actionItem *proto.ActionItemDTO) int64 {
	actionItemId, _ := strconv.ParseInt(actionItem.GetUuid(), 10, 64)
	return actionItemId
}
