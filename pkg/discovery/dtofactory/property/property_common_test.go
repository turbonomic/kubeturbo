package property

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var LABEL1 string = "label1"
var ANNOTATION1 string = "annotation1"
var ANNOTATION2 string = "annotation2"

var MockLabels map[string]string = map[string]string{
	LABEL1: "value",
}

var MockAnnotations map[string]string = map[string]string{
	ANNOTATION1: "value",
	ANNOTATION2: "value",
}

func getPropertyKeys(properties []*proto.EntityDTO_EntityProperty) []string {
	var propertyKeys []string
	for _, property := range properties {
		propertyKeys = append(propertyKeys, property.GetName())
	}
	return propertyKeys
}

func TestBuildLabelAnnotationPropertiesWithoutRegex(t *testing.T) {
	var regex string

	properties := BuildLabelAnnotationProperties(MockLabels, MockAnnotations, regex)

	// Should not contain any annotations when no regex is supplied
	propertyKeys := getPropertyKeys(properties)
	assert.Contains(t, propertyKeys, LABEL1)
	assert.NotContains(t, propertyKeys, ANNOTATION1)
	assert.NotContains(t, propertyKeys, ANNOTATION2)
}

func TestBuildLabelAnnotationPropertiesWithRegex(t *testing.T) {
	var regex string = "^.*2$"

	properties := BuildLabelAnnotationProperties(MockLabels, MockAnnotations, regex)

	// Should only contain annotataions that match the regex
	propertyKeys := getPropertyKeys(properties)
	assert.Contains(t, propertyKeys, LABEL1)
	assert.NotContains(t, propertyKeys, ANNOTATION1)
	assert.Contains(t, propertyKeys, ANNOTATION2)
}
