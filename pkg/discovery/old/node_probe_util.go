package old

import (
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Check if a node is in Ready status.
func NodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

// Check if a node is schedulable.
func NodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected, ideally, we should use "Kubernetes-Node".
	nodePropertyNamespace = "DEFAULT"

	nodePropertyNameNodeName = "KubernetesNodeName"
)

// Build entity properties for a node. The name is the name of the node shown inside Kubernetes cluster.
func BuildNodeProperties(node *api.Node) *proto.EntityDTO_EntityProperty {
	propertyNamespace := nodePropertyNamespace
	propertyName := nodePropertyNameNodeName
	propertyValue := node.Name
	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	}
}

// Get node name from entity property.
func GetNodeNameFromProperty(properties []*proto.EntityDTO_EntityProperty) (nodeName string) {
	if properties == nil {
		return
	}
	for _, property := range properties {
		if property.GetNamespace() != nodePropertyNamespace {
			continue
		}
		if property.GetName() == nodePropertyNameNodeName {
			nodeName = property.GetValue()
			return
		}
	}
	return
}
