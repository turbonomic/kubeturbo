package property

import (
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

const TaintPropertyNamePrefix = "[k8s taint]"

// BuildNodeProperties builds entity properties for a node. It brings over the following 4 things as properties:
// 1. The name of the node shown inside Kubernetes cluster; the property name is "KubernetesNodeName".
// 2. A fully qualified name populated as "<cluster id>/<node name>"; the property name is "KubernetesFullyQualifiedName".
// 3. The labels of the node; each label's key-value pair is directly brought over as tags.
// 4. The taints of the node.
func BuildNodeProperties(node *api.Node, clusterId string) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	properties = append(properties, BuildNodeNameProperty(node.Name),
		BuildFullyQualifiedNameProperty(clusterId+util.NamingQualifierSeparator+node.Name))

	tagsPropertyNamespace := VCTagsPropertyNamespace
	labels := node.GetLabels()
	for label, lval := range labels {
		tagNamePropertyName := GetLabelPropertyName(label)
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
