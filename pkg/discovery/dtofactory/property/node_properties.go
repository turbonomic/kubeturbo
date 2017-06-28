package property

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

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
