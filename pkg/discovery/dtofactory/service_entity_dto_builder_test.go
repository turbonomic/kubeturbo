package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var testService = api.Service{
	Spec: api.ServiceSpec{
		ClusterIP: "10.10.0.1",
		Type:      api.ServiceTypeClusterIP,
	},
}

func createPods() []*api.Pod {
	pod1 := &api.Pod{Status: api.PodStatus{PodIP: "10.1.1.1"}, ObjectMeta: metav1.ObjectMeta{UID: types.UID("1234-456-789")}}
	pod2 := &api.Pod{Status: api.PodStatus{PodIP: "10.1.1.2"}, ObjectMeta: metav1.ObjectMeta{UID: types.UID("5678-1223-1122")}}
	pod3 := &api.Pod{Status: api.PodStatus{PodIP: "10.1.1.3"}, ObjectMeta: metav1.ObjectMeta{UID: types.UID("5636-7799-8838")}}
	return []*api.Pod{pod1, pod2, pod3}
}
func TestBuildServiceData(t *testing.T) {
	serviceData := createServiceData(&testService)
	assert.Equal(t, testService.Spec.ClusterIP, *serviceData.IpAddress)
	assert.NotNil(t, serviceData.GetKubernetesServiceData())
	serviceTypeEnum, _ := proto.EntityDTO_KubernetesServiceData_ServiceType_value[string(testService.Spec.Type)]
	assert.Equal(t,
		proto.EntityDTO_KubernetesServiceData_ServiceType(serviceTypeEnum),
		serviceData.GetKubernetesServiceData().GetServiceType())
}

func TestGetIPProperty(t *testing.T) {
	pod := createPods()
	// Call the function
	result := getIPProperty(pod)

	// Check the result
	expectedNamespace := stitching.DefaultPropertyNamespace
	expectedAttribute := stitching.AppStitchingAttr
	expectedValue := "Service-10.1.1.1-1234,Service-10.1.1.2-5678,Service-10.1.1.3-5636"

	if result.Namespace == nil || *result.Namespace != expectedNamespace {
		t.Errorf("IP property test failed: namespace is incorrect (%v) Vs. (%v)", result.Namespace, expectedNamespace)
	}
	if result.Name == nil || *result.Name != expectedAttribute {
		t.Errorf("IP property test failed: name is incorrect (%v) Vs. (%v)", result.Name, expectedAttribute)
	}
	if result.Value == nil || *result.Value != expectedValue {
		t.Errorf("IP property test failed: value is incorrect (%v) Vs. (%v)", result.Value, expectedValue)
	}
}

func TestGetIPPropertyEmptyInput(t *testing.T) {
	// Create empty input slice of pods
	pods := []*api.Pod{}

	// Call the function
	result := getIPProperty(pods)

	// Check the result
	expectedNamespace := stitching.DefaultPropertyNamespace
	expectedAttribute := stitching.AppStitchingAttr
	expectedValue := ""

	if result.Namespace == nil || *result.Namespace != expectedNamespace {
		t.Errorf("IP property test failed: namespace is incorrect (%v) Vs. (%v)", result.Namespace, expectedNamespace)
	}
	if result.Name == nil || *result.Name != expectedAttribute {
		t.Errorf("IP property test failed: name is incorrect (%v) Vs. (%v)", result.Name, expectedAttribute)
	}
	if result.Value == nil || *result.Value != expectedValue {
		t.Errorf("IP property test failed: value is incorrect (%v) Vs. (%v)", result.Value, expectedValue)
	}
}
