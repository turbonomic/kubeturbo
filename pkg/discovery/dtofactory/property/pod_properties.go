package property

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	podPropertyNamespace = "DEFAULT"

	podPropertyNamePodNamespace = "KubernetesPodNamespace"

	podPropertyNamePodName = "KubernetesPodName"
)

// Build entity properties of a pod. The properties are consisted of name and namespace of a pod.
func BuildPodProperties(pod *api.Pod) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := podPropertyNamespace
	podNamespacePropertyName := podPropertyNamePodNamespace
	podNamespacePropertyValue := pod.Namespace
	namespaceProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamespacePropertyName,
		Value:     &podNamespacePropertyValue,
	}
	properties = append(properties, namespaceProperty)

	podNamePropertyName := podPropertyNamePodName
	podNamePropertyValue := pod.Name
	nameProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamePropertyName,
		Value:     &podNamePropertyValue,
	}
	properties = append(properties, nameProperty)

	return properties
}

// Get the namespace and name of a pod from entity property.
func GetPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (podNamespace string, podName string) {
	if properties == nil {
		return
	}
	for _, property := range properties {
		if property.GetNamespace() != podPropertyNamespace {
			continue
		}
		if podNamespace == "" && property.GetName() == podPropertyNamePodNamespace {
			podNamespace = property.GetValue()
		}
		if podName == "" && property.GetName() == podPropertyNamePodName {
			podName = property.GetValue()
		}
		if podNamespace != "" && podName != "" {
			return
		}
	}
	return
}
