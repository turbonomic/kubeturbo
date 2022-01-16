package property

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	k8sPropertyNamespace    = "DEFAULT"
	VCTagsPropertyNamespace = "VCTAGS"
	k8sNamespace            = "KubernetesNamespace"
	k8sPodName              = "KubernetesPodName"
	k8sNodeName             = "KubernetesNodeName"
	k8sContainerIndex       = "Kubernetes-Container-Index"
	k8sAppNamespace         = "KubernetesAppNamespace"
	k8sAppName              = "KubernetesAppName"
	k8sAppType              = "KubernetesAppType"
)

// Add label and annotation
func BuildLabelAnnotationProperties(tagMaps []map[string]string) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	tagsPropertyNamespace := VCTagsPropertyNamespace

	for _, tagMap := range tagMaps {
		for key, val := range tagMap {
			tagNamePropertyName := key
			tagNamePropertyValue := val
			tagProperty := &proto.EntityDTO_EntityProperty{
				Namespace: &tagsPropertyNamespace,
				Name:      &tagNamePropertyName,
				Value:     &tagNamePropertyValue,
			}
			properties = append(properties, tagProperty)
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
