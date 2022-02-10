package property

import (
	"regexp"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
