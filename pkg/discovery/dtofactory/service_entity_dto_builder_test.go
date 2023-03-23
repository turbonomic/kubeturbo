package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

var testService = api.Service{
	Spec: api.ServiceSpec{
		ClusterIP: "10.10.0.1",
		Type:      api.ServiceTypeClusterIP,
	},
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
