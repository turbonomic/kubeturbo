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
