package property

import (
	"regexp"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	k8sPropertyNamespace         = "DEFAULT"
	VCTagsPropertyNamespace      = "VCTAGS"
	k8sNamespace                 = "KubernetesNamespace"
	k8sPodName                   = "KubernetesPodName"
	k8sNodeName                  = "KubernetesNodeName"
	k8sContainerIndex            = "Kubernetes-Container-Index"
	k8sAppNamespace              = "KubernetesAppNamespace"
	k8sAppName                   = "KubernetesAppName"
	k8sAppType                   = "KubernetesAppType"
	TolerationPropertyNamePrefix = "[k8s toleration]"
	LabelPropertyNamePrefix      = "[k8s label]"
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
