package probe

import (
	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected, ideally, we should use "Kubernetes-Application".
	appPropertyNamespace = "DEFAULT"

	appPropertyNameHostingPodNamespace = "Kubernetes-App-Pod-Namespace"

	appPropertyNameHostingPodName = "Kubernetes-App-Pod-Name"
)

// Build properties of an application. The namespace and name of the hosting pod is stored in the properties.
func buildAppProperties(podNamespaceName string) []*proto.EntityDTO_EntityProperty {
	podNamespace, podName, err := BreakdownPodClusterID(podNamespaceName)
	if err != nil {
		glog.Errorf("Failed to build App properties: %s", err)
		return nil
	}
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := appPropertyNamespace
	hostingPodNamespacePropertyName := appPropertyNameHostingPodNamespace
	hostingPodNamespacePropertyValue := podNamespace
	namespaceProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &hostingPodNamespacePropertyName,
		Value:     &hostingPodNamespacePropertyValue,
	}
	properties = append(properties, namespaceProperty)

	hostingPodNamePropertyName := appPropertyNameHostingPodName
	hostingPodNamePropertyValue := podName
	nameProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &hostingPodNamePropertyName,
		Value:     &hostingPodNamePropertyValue,
	}
	properties = append(properties, nameProperty)

	return properties
}

// Get the namespace and name of the pod, which hosts the application, from the properties of the application.
func GetApplicationHostingPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (
	hostingPodNamespace string, hostingPodName string) {
	if properties == nil {
		return
	}
	for _, property := range properties {
		if property.GetNamespace() != appPropertyNamespace {
			continue
		}
		if hostingPodNamespace == "" && property.GetName() == appPropertyNameHostingPodNamespace {
			hostingPodNamespace = property.GetValue()
		}
		if hostingPodName == "" && property.GetName() == appPropertyNameHostingPodName {
			hostingPodName = property.GetValue()
		}
		if hostingPodNamespace != "" && hostingPodName != "" {
			return
		}
	}
	return
}
