package resourcemapping

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"testing"
)

var (
	resourceMappingTemplate = map[string]interface{}{
		"srcPath":  `.spec.template.spec.containers[?(@.name=="{{.componentName}}")].resources`,
		"destPath": `.spec.{{.componentName}}.resources`,
	}
)

func TestORMClient_populateORMTemplateMap(t *testing.T) {

	testORMCR := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "turbonomic.com/v1alpha1",
			"kind":       "OperatorResourceMapping",
			"metadata": map[string]interface{}{
				"name": "xls.charts.helm.k8s.io",
			},
			"spec": map[string]interface{}{
				"resourceMappings": []interface{}{
					map[string]interface{}{
						"srcResourceSpec": map[string]interface{}{
							"kind": "Deployment",
							"componentNames": []interface{}{
								"action-orchestrator",
								"api",
							},
						},
						"resourceMappingTemplates": []interface{}{
							resourceMappingTemplate,
						},
					},
				},
			},
		},
	}

	expectedOrmTemplateMap := map[string]ORMTemplate{
		"Deployment/action-orchestrator": {
			componentName: "action-orchestrator",
			resourceMappingTemplates: []map[string]interface{}{
				resourceMappingTemplate,
			},
		},
		"Deployment/api": {
			componentName: "api",
			resourceMappingTemplates: []map[string]interface{}{
				resourceMappingTemplate,
			},
		},
	}

	testORMClient := NewORMClient(nil, nil)
	ormTemplateMap, _ := testORMClient.populateORMTemplateMap(testORMCR)
	if !reflect.DeepEqual(expectedOrmTemplateMap, ormTemplateMap) {
		t.Errorf("Test case failed: TestORMClient_populateORMTemplateMap:\nexpected:\n%++v\nactual:\n%++v",
			expectedOrmTemplateMap, ormTemplateMap)
	}
}

func TestORMClient_parseSrcAndDestPath(t *testing.T) {
	testORMClient := NewORMClient(nil, nil)

	resourceMappingTemplate2 := make(map[string]interface{})
	for k, v := range resourceMappingTemplate {
		resourceMappingTemplate2[k] = v
	}
	resourceMappingTemplate2[resourceMappingComponentName] = "api"
	resourceMappingTemplate3 := make(map[string]interface{})

	tests := []struct {
		name                    string
		resourceMappingTemplate map[string]interface{}
		expectSrcPath           string
		expectDestPath          string
		expectErr               bool
	}{
		{
			name:                    "parseSrcAndDestPath",
			resourceMappingTemplate: resourceMappingTemplate2,
			expectSrcPath:           `.spec.template.spec.containers[?(@.name=="api")].resources`,
			expectDestPath:          ".spec.api.resources",
			expectErr:               false,
		},
		{
			name:                    "parseSrcAndDestPathNoValue",
			resourceMappingTemplate: resourceMappingTemplate,
			expectSrcPath:           `.spec.template.spec.containers[?(@.name=="<no value>")].resources`,
			expectDestPath:          `.spec.<no value>.resources`,
			expectErr:               false,
		},
		{
			name:                    "parseSrcAndDestPathWithErr",
			resourceMappingTemplate: resourceMappingTemplate3,
			expectSrcPath:           "",
			expectDestPath:          "",
			expectErr:               true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srcPath, destPath, err := testORMClient.parseSrcAndDestPath(test.resourceMappingTemplate)
			if (err != nil) != test.expectErr {
				t.Errorf("parseSrcAndDestPath() error = %v, expectErr %v", err, test.expectErr)
				return
			}
			if srcPath != test.expectSrcPath {
				t.Errorf("parseSrcAndDestPath() expectSrcPath = %v, expectSrcPath %v", srcPath, test.expectSrcPath)
			}
			if destPath != test.expectDestPath {
				t.Errorf("parseSrcAndDestPath() expectDestPath = %v, expectSrcPath %v", destPath, test.expectDestPath)
			}
		})
	}
}
