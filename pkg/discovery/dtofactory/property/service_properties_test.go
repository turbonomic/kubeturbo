package property

import (
	"testing"

	api "k8s.io/api/core/v1"
)

// Test service with multiple labels
func TestBuildServiceLabelProperties(t *testing.T) {
	service := &api.Service{}
	service.SetLabels(map[string]string{
		"app": "myapp",
	})

	expectedName := GetLabelPropertyName("app")
	expectedValue := "myapp"

	properties := BuildServiceLabelProperties(service)

	if len(properties) != 1 {
		t.Errorf("Expected 1 property, got %d", len(properties))
		return
	}

	actualProperty := properties[0]

	if VCTagsPropertyNamespace != *actualProperty.Namespace {
		t.Errorf("Expected namespace %s, got %s", VCTagsPropertyNamespace, *actualProperty.Namespace)
	}

	if expectedName != *actualProperty.Name {
		t.Errorf("Expected name %s, got %s", expectedName, *actualProperty.Name)
	}

	if expectedValue != *actualProperty.Value {
		t.Errorf("Expected value %s, got %s", expectedValue, *actualProperty.Value)
	}

}

// Test service with no labels
func TestBuildServiceLabelPropertiesNoLabels(t *testing.T) {
	service := &api.Service{}
	properties := BuildServiceLabelProperties(service)

	if len(properties) != 0 {
		t.Errorf("Expected no properties, got %d", len(properties))
		return
	}
}
