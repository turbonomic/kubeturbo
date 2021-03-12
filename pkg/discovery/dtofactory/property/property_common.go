package property

import "github.com/turbonomic/turbo-go-sdk/pkg/proto"

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
