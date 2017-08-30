package property

import (
	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"strconv"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected, ideally, we should use "Kubernetes-Application".
	appPropertyNamespace = "DEFAULT"

	appPropertyNameHostingPodNamespace   = "Kubernetes-App-Pod-Namespace"
	appPropertyNameHostingPodName        = "Kubernetes-App-Pod-Name"
	appPropertyNameHostingContainerIndex = "Kubernetes-App-Container-Index"
)

// Build properties of an application. The namespace and name of the hosting pod is stored in the properties.
func BuildAppProperties(podNamespace, podName string, index int) []*proto.EntityDTO_EntityProperty {

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

	containerIndexName := appPropertyNameHostingContainerIndex
	containerIndexValue := strconv.Itoa(index)
	indexProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &containerIndexName,
		Value:     &containerIndexValue,
	}
	properties = append(properties, indexProperty)

	return properties
}

// Get the namespace and name of the pod, which hosts the application, from the properties of the application.
func GetApplicationHostingPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (
	hostingPodNamespace string, hostingPodName string, index int) {
	if properties == nil {
		return
	}

	index = 0
	dict := make(map[string]struct{})
	dict[appPropertyNameHostingPodNamespace] = struct{}{}
	dict[appPropertyNameHostingPodName] = struct{}{}
	dict[appPropertyNameHostingContainerIndex] = struct{}{}

	for _, property := range properties {
		if property.GetNamespace() != appPropertyNamespace {
			continue
		}
		name := property.GetName()
		if _, exist := dict[name]; !exist {
			continue
		}
		delete(dict, name)
		value := property.GetValue()

		switch name {
		case appPropertyNameHostingPodNamespace:
			hostingPodNamespace = value
		case appPropertyNameHostingPodName:
			hostingPodName = value
		case appPropertyNameHostingContainerIndex:
			tmp, err := strconv.Atoi(value)
			if err != nil {
				glog.Errorf("convert containerIndex[%s] failed: %v", value, err)
				tmp = -1
			}
			index = tmp
		default:
			glog.Warningf("Potential bug: %s", name)
		}

		if len(dict) < 1 {
			return
		}
	}
	return
}
