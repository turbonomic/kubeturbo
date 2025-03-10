package property

import (
	"fmt"
	"regexp"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	k8sPropertyNamespace         = "DEFAULT"
	VCTagsPropertyNamespace      = "VCTAGS"
	k8sFullyQualifiedName        = "KubernetesFullyQualifiedName"
	k8sNamespace                 = "KubernetesNamespace"
	k8sServiceName               = "KubernetesServiceName"
	k8sWorkloadControllerName    = "KubernetesWorkloadControllerName"
	k8sPodName                   = "KubernetesPodName"
	k8sContainerName             = "KubernetesContainerName"
	k8sNodeName                  = "KubernetesNodeName"
	k8sContainerIndex            = "Kubernetes-Container-Index"
	k8sAppNamespace              = "KubernetesAppNamespace"
	k8sAppName                   = "KubernetesAppName"
	k8sAppType                   = "KubernetesAppType"
	TolerationPropertyNamePrefix = "[k8s toleration]"
	LabelPropertyNamePrefix      = "[k8s label]"
	k8sVolumeAttached            = "PersistentVolumeAttached"
	IsVirtualMachineInstance     = "IsVirtualMachineInstance"
)

func BuildTagProperty(namespace string, name string, value string) *proto.EntityDTO_EntityProperty {
	return &proto.EntityDTO_EntityProperty{
		Namespace: &namespace,
		Name:      &name,
		Value:     &value,
	}
}

// Add label and annotation
func BuildLabelAnnotationProperties(labelMap map[string]string, annotationMap map[string]string, annotationRegex string) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	tagsPropertyNamespace := VCTagsPropertyNamespace

	// Add labels
	for key, val := range labelMap {
		tagProperty := BuildTagProperty(tagsPropertyNamespace, key, val)
		properties = append(properties, tagProperty)
	}

	// Add annotations
	if len(annotationRegex) > 0 { // for some reason a regex that's an empty string matches everything ¯\_(ツ)_/¯
		r, err := regexp.Compile(annotationRegex)
		if err == nil {
			for key, val := range annotationMap {
				// Only add annotations that match the supplied regex
				if r.MatchString(key) {
					tagProperty := BuildTagProperty(tagsPropertyNamespace, key, val)
					properties = append(properties, tagProperty)
				}
			}
		}
	}

	return properties
}

// Creates the properties identifying mapped application namespace, name and type
func BuildBusinessAppRelatedProperties(app repository.K8sApp) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := k8sPropertyNamespace

	appPropertyNamespaceKey := k8sAppNamespace
	appPropertyNamespaceValue := app.Namespace
	properties = append(properties, &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &appPropertyNamespaceKey,
		Value:     &appPropertyNamespaceValue,
	})

	appPropertyNameKey := k8sAppName
	appPropertyNameValue := app.Name
	properties = append(properties, &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &appPropertyNameKey,
		Value:     &appPropertyNameValue,
	})

	appPropertyTypeKey := k8sAppType
	appPropertyTypeValue := app.Type
	properties = append(properties, &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &appPropertyTypeKey,
		Value:     &appPropertyTypeValue,
	})

	return properties
}

// Creates the properties identifying mapped application namespace, name and type
func GetManagerAppFromProperties(properties []*proto.EntityDTO_EntityProperty) *repository.K8sApp {
	managerApp := &repository.K8sApp{}
	if properties == nil {
		return nil
	}

	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		if (property.GetName() != k8sAppNamespace) &&
			(property.GetName() != k8sAppName) &&
			(property.GetName() != k8sAppType) {
			continue
		}

		if property.GetName() == k8sAppType {
			if property.GetValue() != repository.AppTypeArgoCD {
				// We return a valid app only for app type argoCD as of now
				continue
			}
			managerApp.Type = property.GetValue()
		}

		if property.GetName() == k8sAppName {
			managerApp.Name = property.GetValue()
		}

		if property.GetName() == k8sAppNamespace {
			managerApp.Namespace = property.GetValue()
		}
	}

	if managerApp.Type == "" {
		return nil
	}
	return managerApp
}

// BuildNamespaceProperty builds an entity property storing the Kubernetes namespace.
func BuildNamespaceProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sNamespace, name)
}

// BuildServiceNameProperty builds an entity property storing the Kubernetes service name.
func BuildServiceNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sServiceName, name)
}

// BuildWorkloadControllerNameProperty builds an entity property storing the Kubernetes workload controller name.
func BuildWorkloadControllerNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sWorkloadControllerName, name)
}

// BuildContainerNameProperty builds an entity property storing the Kubernetes container name.
func BuildContainerNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sContainerName, name)
}

// BuildContainerIndexProperty builds an entity property storing the Kubernetes container index.
func BuildContainerIndexProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sContainerIndex, name)
}

// BuildPodNameProperty builds an entity property storing the Kubernetes pod name.
func BuildPodNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sPodName, name)
}

// BuildNodeNameProperty builds an entity property storing the Kubernetes node name.
func BuildNodeNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sNodeName, name)
}

// BuildFullyQualifiedNameProperty builds an entity property storing the Kubernetes fully qualified name for the entity.
func BuildFullyQualifiedNameProperty(name string) *proto.EntityDTO_EntityProperty {
	return BuildTagProperty(k8sPropertyNamespace, k8sFullyQualifiedName, name)
}

// getK8sProperty gets the property in the k8s namespace by the property name.
func getK8sProperty(properties []*proto.EntityDTO_EntityProperty, propertyName string) (*proto.EntityDTO_EntityProperty, error) {
	if properties == nil {
		return nil, fmt.Errorf("the list of given entity properties is empty: %v", properties)
	}

	for _, property := range properties {
		if property.GetNamespace() != k8sPropertyNamespace {
			continue
		}
		if property.GetName() == propertyName && property.GetValue() != "" {
			// return the first non-empty value found
			return property, nil
		}
	}
	return nil, fmt.Errorf("property value of %s is not found or is empty", propertyName)
}

// getK8sPropertyValueByName gets the property value in the k8s namespace by the property name.
func getK8sPropertyValueByName(properties []*proto.EntityDTO_EntityProperty, propertyName string) (string, error) {
	if property, err := getK8sProperty(properties, propertyName); err != nil {
		return "", err
	} else {
		return property.GetValue(), nil
	}
}

// GetNamespaceFromProperties gets the namespace name from the entity properties.
func GetNamespaceFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sNamespace)
}

// getServiceNameFromProperties gets the service name from the entity properties.
func getServiceNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sServiceName)
}

// GetWorkloadControllerNameFromProperties gets the workload controller name from the entity properties.
func GetWorkloadControllerNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sWorkloadControllerName)
}

// GetPodNameFromProperties gets the pod name from the entity properties.
func GetPodNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sPodName)
}

// GetContainerNameFromProperties gets the container name from the entity properties.
func GetContainerNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sContainerName)
}

// GetContainerIndexFromProperties gets the container index from the entity properties.
func getContainerIndexFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sContainerIndex)
}

// GetNodeNameFromProperties gets the node name from the entity properties.
func GetNodeNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sNodeName)
}

// GetFullyQualifiedNameFromProperties gets the k8s fully qualified name from the entity properties.
func GetFullyQualifiedNameFromProperties(properties []*proto.EntityDTO_EntityProperty) (string, error) {
	return getK8sPropertyValueByName(properties, k8sFullyQualifiedName)
}

// GetLabelPropertyName gets the label property name for the given label.
func GetLabelPropertyName(label string) string {
	return LabelPropertyNamePrefix + " " + label
}
