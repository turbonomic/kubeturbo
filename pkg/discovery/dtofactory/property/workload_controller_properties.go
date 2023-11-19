package property

import (
	"fmt"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Build entity properties of a pod. The properties are consisted of name and namespace of a pod.
func BuildWorkloadControllerNSProperty(namespace string) *proto.EntityDTO_EntityProperty {
	propertyNamespace := k8sPropertyNamespace
	podNamespacePropertyName := k8sNamespace
	podNamespacePropertyValue := namespace
	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamespacePropertyName,
		Value:     &podNamespacePropertyValue,
	}
}

// Get the namespace and name of a pod from entity property.
func GetWorkloadNamespaceFromProperty(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	namespace := ""

	if properties == nil {
		return namespace, fmt.Errorf("empty")
	}

	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		if namespace == "" && property.GetName() == k8sNamespace {
			namespace = property.GetValue()
		}
		if namespace != "" {
			return namespace, nil
		}
	}

	if len(namespace) < 1 {
		return "", fmt.Errorf("podNamespace is empty")
	}

	return namespace, nil
}
