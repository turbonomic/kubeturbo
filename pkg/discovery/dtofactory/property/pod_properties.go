package property

import (
	api "k8s.io/api/core/v1"

	"fmt"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	k8sPropertyNamespace = "DEFAULT"
	k8sNamespace         = "KubernetesNamespace"
	k8sPodName           = "KubernetesPodName"
	k8sNodeName          = "KubernetesNodeName"
	k8sContainerIndex    = "Kubernetes-Container-Index"
)

// Build entity properties of a pod. The properties are consisted of name and namespace of a pod.
func BuildPodProperties(pod *api.Pod) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := k8sPropertyNamespace
	podNamespacePropertyName := k8sNamespace
	podNamespacePropertyValue := pod.Namespace
	namespaceProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamespacePropertyName,
		Value:     &podNamespacePropertyValue,
	}
	properties = append(properties, namespaceProperty)

	podNamePropertyName := k8sPodName
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
func GetPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (string, string, error) {
	podNamespace := ""
	podName := ""

	if properties == nil {
		return podNamespace, podName, fmt.Errorf("empty")
	}

	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		if podNamespace == "" && property.GetName() == k8sNamespace {
			podNamespace = property.GetValue()
		}
		if podName == "" && property.GetName() == k8sPodName {
			podName = property.GetValue()
		}
		if podNamespace != "" && podName != "" {
			return podNamespace, podName, nil
		}
	}

	if len(podNamespace) < 1 {
		return "", "", fmt.Errorf("podNamespace is empty")
	}

	if len(podName) < 1 {
		return "", "", fmt.Errorf("podName is empty")
	}

	return podNamespace, podName, nil
}
