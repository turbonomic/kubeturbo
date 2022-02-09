package property

import (
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sOrchestratorName  = "orchestrator"
	k8sOrchestratorValue = "Kubernetes"
)

// Build entity properties for a node. The name is the name of the node shown inside Kubernetes cluster.
func BuildNodeProperties(node *api.Node) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := k8sPropertyNamespace
	propertyName := k8sNodeName
	propertyValue := node.Name
	nameProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	}
	properties = append(properties, nameProperty)

	tagsPropertyNamespace := VCTagsPropertyNamespace
	labels := node.GetLabels()
	for label, lval := range labels {
		tagNamePropertyName := label
		tagNamePropertyValue := lval
		tagProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &tagsPropertyNamespace,
			Name:      &tagNamePropertyName,
			Value:     &tagNamePropertyValue,
		}
		properties = append(properties, tagProperty)
	}

	orchestratorNamespace := VCTagsPropertyNamespace
	orchestratorName := k8sOrchestratorName
	orchestratorValue := k8sOrchestratorValue
	orchestratorProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &orchestratorNamespace,
		Name:      &orchestratorName,
		Value:     &orchestratorValue,
	}

	properties = append(properties, orchestratorProperty)
	return properties
}

// Get node name from entity property.
func GetNodeNameFromProperty(properties []*proto.EntityDTO_EntityProperty) (nodeName string) {
	if properties == nil {
		return
	}
	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		if property.GetName() == k8sNodeName {
			nodeName = property.GetValue()
			return
		}
	}
	return
}
