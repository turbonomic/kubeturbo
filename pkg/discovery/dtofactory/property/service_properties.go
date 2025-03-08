package property

import (
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

// BuildServiceLabelProperties builds label properties for a service.
func BuildServiceLabelProperties(service *api.Service) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty

	tagsPropertyNamespace := VCTagsPropertyNamespace
	labels := service.GetLabels()
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

	return properties
}
