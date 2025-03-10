package custom

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	k8sapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestKubevirtVirtualMachineTestSuite(t *testing.T) {
	suite.Run(t, new(KubevirtVirtualMachineTestSuite))
}

type KubevirtVirtualMachineTestSuite struct {
	suite.Suite
}

func (suite *KubevirtVirtualMachineTestSuite) TestGeneratePodSpecForCustomController_withInvalidKind() {
	updated, err := GeneratePodSpecForCustomController("UnknownKind", make(map[string]interface{}), &k8sapi.PodSpec{})

	assert.Nil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestGeneratePodSpecForCustomController_withNilPodspec() {
	updated, err := GeneratePodSpecForCustomController(util.KindVirtualMachine, make(map[string]interface{}), nil)

	assert.NotNil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestGeneratePodSpecForCustomController_withEmptyObj() {
	updated, err := GeneratePodSpecForCustomController(util.KindVirtualMachine, make(map[string]interface{}), &k8sapi.PodSpec{})

	assert.Nil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestGeneratePodSpecForCustomController() {
	podSpec := &k8sapi.PodSpec{}
	updated, err := GeneratePodSpecForCustomController(util.KindVirtualMachine, createVmObjWithData(), podSpec)

	assert.Nil(suite.T(), err)
	assert.True(suite.T(), updated)
	assert.Equal(suite.T(), util.ComputeContainerName, podSpec.Containers[0].Name)
	assert.Equal(suite.T(), float64(2147483648), podSpec.Containers[0].Resources.Limits.Memory().AsApproximateFloat64())
	assert.Equal(suite.T(), float64(2), podSpec.Containers[0].Resources.Limits.Cpu().AsApproximateFloat64())
	assert.Equal(suite.T(), float64(1073741824), podSpec.Containers[0].Resources.Requests.Memory().AsApproximateFloat64())
	assert.Equal(suite.T(), float64(1), podSpec.Containers[0].Resources.Requests.Cpu().AsApproximateFloat64())
}

func createVmObjWithData() map[string]interface{} {
	return map[string]interface{}{
		"kind": util.KindVirtualMachine,
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"domain": map[string]interface{}{
						"resources": createResourcesDataMap(),
					},
				},
			},
		},
	}
}

func createResourcesDataMap() map[string]interface{} {
	return map[string]interface{}{
		"requests": map[string]interface{}{
			"memory": "1Gi",
			"cpu":    "1",
		},
		"limits": map[string]interface{}{
			"memory": "2Gi",
			"cpu":    "2",
		},
	}
}

func (suite *KubevirtVirtualMachineTestSuite) TestCheckAndUpdateCustomController_withInvalidKind() {
	_, updated, err := CheckAndUpdateCustomController("UnknownKind", &unstructured.Unstructured{}, &k8sapi.PodSpec{})

	assert.Nil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestCheckAndUpdateCustomController_withNilObj() {
	_, updated, err := CheckAndUpdateCustomController(util.KindVirtualMachine, nil, &k8sapi.PodSpec{})

	assert.NotNil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestCheckAndUpdateCustomController_withNilPodspec() {
	_, updated, err := CheckAndUpdateCustomController(util.KindVirtualMachine, &unstructured.Unstructured{}, nil)

	assert.NotNil(suite.T(), err)
	assert.False(suite.T(), updated)
}

func (suite *KubevirtVirtualMachineTestSuite) TestCheckAndUpdateCustomController() {
	obj := make(map[string]interface{})
	podSpec := generateValidPodSpec()

	updatedObj, updated, err := CheckAndUpdateCustomController(util.KindVirtualMachine, &unstructured.Unstructured{Object: obj}, podSpec)

	assert.Nil(suite.T(), err)
	assert.True(suite.T(), updated)
	suite.assertThatObjWasUpdatedCorrectly(updatedObj)
}

func (suite *KubevirtVirtualMachineTestSuite) assertThatObjWasUpdatedCorrectly(actualObj *unstructured.Unstructured) {
	spec := actualObj.Object["spec"]
	specMap := spec.(map[string]interface{})
	actualRunningValue := specMap["running"].(bool)
	assert.False(suite.T(), actualRunningValue)
	template := specMap["template"].(map[string]interface{})
	templateSpec := template["spec"].(map[string]interface{})
	domain := templateSpec["domain"].(map[string]interface{})
	actualResources := domain["resources"].(map[string]interface{})
	assert.Equal(suite.T(), createResourcesDataMap(), actualResources)
}

func generateValidPodSpec() *k8sapi.PodSpec {
	podSpec := &k8sapi.PodSpec{}
	podSpec.Containers = []k8sapi.Container{
		{
			Name: util.ComputeContainerName,
			Resources: k8sapi.ResourceRequirements{
				Limits: map[k8sapi.ResourceName]resource.Quantity{
					"memory": resource.MustParse("2Gi"),
					"cpu":    resource.MustParse("2"),
				},
				Requests: map[k8sapi.ResourceName]resource.Quantity{
					"memory": resource.MustParse("1Gi"),
					"cpu":    resource.MustParse("1"),
				},
			},
		},
	}
	return podSpec
}

func (suite *KubevirtVirtualMachineTestSuite) TestCompleteCustomController() {
	cli := &mockCli{}
	cli.On("Get", context.TODO(), "", metav1.GetOptions{}, []string(nil)).Return(createUnstructuredObjWithData(), nil)
	expected := createUnstructuredObjWithData()
	objSpec := expected.Object["spec"].(map[string]interface{})
	objSpec["running"] = true
	cli.On("Update", context.TODO(), expected, metav1.UpdateOptions{}, []string(nil)).Return(createUnstructuredObjWithData(), nil)

	obj := make(map[string]interface{})
	err := CompleteCustomController(cli, util.KindVirtualMachine, &unstructured.Unstructured{Object: obj})

	assert.Nil(suite.T(), err)
}

func createUnstructuredObjWithData() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"running": false,
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"domain": map[string]interface{}{
						"resources": map[string]interface{}{
							"limits": map[string]interface{}{
								"memory": "2Gi",
								"cpu":    "2",
							},
							"requests": map[string]interface{}{
								"memory": "1Gi",
								"cpu":    "1",
							},
						},
					},
				},
			},
		},
	}}
}
