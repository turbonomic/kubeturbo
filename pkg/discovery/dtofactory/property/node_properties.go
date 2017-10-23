package property

import (
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Build entity properties for a node. The name is the name of the node shown inside Kubernetes cluster.
func BuildNodeProperties(node *api.Node) *proto.EntityDTO_EntityProperty {
	propertyNamespace := k8sPropertyNamespace
	propertyName := k8sNodeName
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
		if property.GetNamespace() != k8sNamespace {
			continue
		}
		if property.GetName() == k8sNodeName {
			nodeName = property.GetValue()
			return
		}
	}
	return
}
