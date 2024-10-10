package property

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	testNamespaceName          = "lion namespace"
	testServiceName            = "dragon service"
	testWorkloadControllerName = "monkey workload controller"
	testContainerName          = "panda container"
	testContainerIndex         = "2"
	testPodName                = "butterfly pod"
	testNodeName               = "eagle node"
	testFullyQualifiedName     = "tiger/lion/monkey/panda"
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

	// Should only contain annotations that match the regex
	propertyKeys := getPropertyKeys(properties)
	assert.Contains(t, propertyKeys, LABEL1)
	assert.NotContains(t, propertyKeys, ANNOTATION1)
	assert.Contains(t, propertyKeys, ANNOTATION2)
}

func TestK8sProperties(t *testing.T) {
	// building...
	var entityProps []*proto.EntityDTO_EntityProperty
	entityProps = append(entityProps, BuildNamespaceProperty(testNamespaceName))
	entityProps = append(entityProps, BuildServiceNameProperty(testServiceName))
	entityProps = append(entityProps, BuildWorkloadControllerNameProperty(testWorkloadControllerName))
	entityProps = append(entityProps, BuildContainerNameProperty(testContainerName))
	entityProps = append(entityProps, BuildContainerIndexProperty(testContainerIndex))
	entityProps = append(entityProps, BuildPodNameProperty(testPodName))
	entityProps = append(entityProps, BuildNodeNameProperty(testNodeName))
	entityProps = append(entityProps, BuildFullyQualifiedNameProperty(testFullyQualifiedName))

	// verifying...
	retrievedNamespaceName, err := GetNamespaceFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the namespace name from the entity properties")
	assert.Equal(t, testNamespaceName, retrievedNamespaceName,
		"Namespace name from the entity properties doesn't match with expected")

	retrievedServiceName, err := getServiceNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the service name from the entity properties")
	assert.Equal(t, testServiceName, retrievedServiceName,
		"Service name from the entity properties doesn't match with expected")

	retrievedWorkloadControllerName, err := GetWorkloadControllerNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the workload controller name from the entity properties")
	assert.Equal(t, testWorkloadControllerName, retrievedWorkloadControllerName,
		"Workload controller name from the entity properties doesn't match with expected")

	retrievedContainerName, err := GetContainerNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the container name from the entity properties")
	assert.Equal(t, testContainerName, retrievedContainerName,
		"Container name from the entity properties doesn't match with expected")

	retrievedContainerIndex, err := getContainerIndexFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the container index from the entity properties")
	assert.Equal(t, testContainerIndex, retrievedContainerIndex,
		"Container index from the entity properties doesn't match with expected")

	retrievedPodName, err := GetPodNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the pod name from the entity properties")
	assert.Equal(t, testPodName, retrievedPodName,
		"Pod name from the entity properties doesn't match with expected")

	retrievedNodeName, err := GetNodeNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the node name from the entity properties")
	assert.Equal(t, testNodeName, retrievedNodeName,
		"Node name from the entity properties doesn't match with expected")

	retrievedFullyQualifiedName, err := GetFullyQualifiedNameFromProperties(entityProps)
	assert.Nil(t, err, "Error getting the fully qualified name from the entity properties")
	assert.Equal(t, testFullyQualifiedName, retrievedFullyQualifiedName,
		"Fully qualified name from the entity properties doesn't match with expected")
}
