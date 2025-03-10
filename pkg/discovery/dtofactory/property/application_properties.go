package property

import (
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// AddContainerProperties builds properties of the container for an application.
// These properties include: namespace, pod name, container name, container index, and a fully qualified name prefixed
// by the cluster id.
func AddContainerProperties(clusterId, podNamespace, podName, containerName string, index int) []*proto.EntityDTO_EntityProperty {

	var properties []*proto.EntityDTO_EntityProperty

	fqn := strings.Join([]string{clusterId, podNamespace, podName, containerName}, util.NamingQualifierSeparator)
	properties = append(properties, BuildFullyQualifiedNameProperty(fqn))
	properties = append(properties, BuildNamespaceProperty(podNamespace))
	properties = append(properties, BuildPodNameProperty(podName))
	properties = append(properties, BuildContainerNameProperty(containerName))
	properties = append(properties, BuildContainerIndexProperty(strconv.Itoa(index)))

	return properties
}

// GetContainerProperties gets the namespace, pod name, container name, and container index from the entity properties.
func GetContainerProperties(properties []*proto.EntityDTO_EntityProperty) (
	hostingPodNamespace, hostingPodName, containerName string, index int) {
	if properties == nil {
		return
	}

	index = 0
	dict := make(map[string]struct{})
	dict[k8sNamespace] = struct{}{}
	dict[k8sPodName] = struct{}{}
	dict[k8sContainerName] = struct{}{}
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
		case k8sContainerName:
			containerName = value
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
