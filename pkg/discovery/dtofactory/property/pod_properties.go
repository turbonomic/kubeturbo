package property

import (
	"fmt"
	"strconv"
	"strings"

	api "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// BuildPodProperties builds entity properties of a pod. The properties consist of name and namespace of a pod.
func BuildPodProperties(pod *api.Pod, clusterName string) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	fqn := strings.Join([]string{clusterName, pod.Namespace, pod.Name}, util.NamingQualifierSeparator)
	properties = append(properties, BuildFullyQualifiedNameProperty(fqn))
	properties = append(properties, BuildNamespaceProperty(pod.Namespace))
	properties = append(properties, BuildPodNameProperty(pod.Name))

	tagsPropertyNamespace := VCTagsPropertyNamespace
	labels := pod.GetLabels()
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

	for _, toleration := range pod.Spec.Tolerations {
		tagNamePropertyName := TolerationPropertyNamePrefix
		if string(toleration.Effect) != "" {
			tagNamePropertyName += " " + string(toleration.Effect)
		}
		var tagNamePropertyValue string
		switch toleration.Operator {
		// empty operator means Equal
		case "", api.TolerationOpEqual:
			tagNamePropertyValue = toleration.Key
			if toleration.Value != "" {
				tagNamePropertyValue += "=" + toleration.Value
			}
		default:
			tagNamePropertyValue = string(toleration.Operator)
			if toleration.Key != "" {
				tagNamePropertyValue = toleration.Key + " " + tagNamePropertyValue
			}
			if toleration.Value != "" {
				tagNamePropertyValue += " " + toleration.Value
			}
		}

		if toleration.TolerationSeconds != nil {
			tagNamePropertyValue += " for " + strconv.FormatInt(*toleration.TolerationSeconds, 10) + "s"
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

// Add volume property to a pod's properties
func AddVolumeProperties(properties []*proto.EntityDTO_EntityProperty) []*proto.EntityDTO_EntityProperty {
	volumePropertyNamespace := VCTagsPropertyNamespace
	volumePropertyName := k8sVolumeAttached
	volumePropertyValue := "true"
	volumeProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &volumePropertyNamespace,
		Name:      &volumePropertyName,
		Value:     &volumePropertyValue,
	}
	properties = append(properties, volumeProperty)
	return properties
}

func AddVirtualMachineInstanceProperties(properties []*proto.EntityDTO_EntityProperty, value string) []*proto.EntityDTO_EntityProperty {
	VMIProperty := BuildTagProperty(VCTagsPropertyNamespace, IsVirtualMachineInstance, value)
	properties = append(properties, VMIProperty)
	return properties
}
