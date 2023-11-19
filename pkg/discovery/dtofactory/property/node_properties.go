package property

import (
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

const TaintPropertyNamePrefix = "[k8s taint]"

// BuildNodeProperties builds entity properties for a node. It brings over the following 3 things as properties:
// 1. The name of the node shown inside Kubernetes cluster; the property name is "KubernetesNodeName".
// 2. The labels of the node; each label's key-value pair is directly brought over as tags.
// 3. The taints of the node.
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
		tagNamePropertyName := LabelPropertyNamePrefix + " " + label
		tagNamePropertyValue := lval
		tagProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &tagsPropertyNamespace,
			Name:      &tagNamePropertyName,
			Value:     &tagNamePropertyValue,
		}
		properties = append(properties, tagProperty)
	}
	for _, taint := range node.Spec.Taints {
		tagNamePropertyName := TaintPropertyNamePrefix
		if string(taint.Effect) != "" {
			tagNamePropertyName += " " + string(taint.Effect)
		}
		tagNamePropertyValue := taint.Key
		if taint.Value != "" {
			tagNamePropertyValue += "=" + taint.Value
		}
		tagProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &tagsPropertyNamespace,
			Name:      &tagNamePropertyName,
			Value:     &tagNamePropertyValue,
		}
		properties = append(properties, tagProperty)
	}

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
