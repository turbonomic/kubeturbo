package property

import (
	"strconv"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Build properties of an application. The namespace and name of the hosting pod is stored in the properties.
func AddHostingPodProperties(podNamespace, podName string, index int) []*proto.EntityDTO_EntityProperty {

	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := k8sPropertyNamespace
	hostingPodNamespacePropertyName := k8sNamespace
	hostingPodNamespacePropertyValue := podNamespace
	namespaceProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &hostingPodNamespacePropertyName,
		Value:     &hostingPodNamespacePropertyValue,
	}
	properties = append(properties, namespaceProperty)

	hostingPodNamePropertyName := k8sPodName
	hostingPodNamePropertyValue := podName
	nameProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &hostingPodNamePropertyName,
		Value:     &hostingPodNamePropertyValue,
	}
	properties = append(properties, nameProperty)

	containerIndexName := k8sContainerIndex
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
func GetHostingPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (
	hostingPodNamespace string, hostingPodName string, index int) {
	if properties == nil {
		return
	}

	index = 0
	dict := make(map[string]struct{})
	dict[k8sNamespace] = struct{}{}
	dict[k8sPodName] = struct{}{}
	dict[k8sContainerIndex] = struct{}{}

	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		name := property.GetName()
		if _, exist := dict[name]; !exist {
			continue
		}
		delete(dict, name)
		value := property.GetValue()

		switch name {
		case k8sNamespace:
			hostingPodNamespace = value
		case k8sPodName:
			hostingPodName = value
		case k8sContainerIndex:
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
