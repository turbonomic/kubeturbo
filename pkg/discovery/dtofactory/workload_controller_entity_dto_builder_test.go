package dtofactory

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"testing"
)

var (
	testCustomControllerType = "CustomControllerType"
	testClusterName          = "cluster"
	testNamespace            = "namespace"
	testNamespaceUID         = "namespace-UID"
	testResizable            = false

	testAllocationResources = []*repository.KubeDiscoveredResource{
		{
			Type:     metrics.CPULimitQuota,
			Capacity: 5327.0,
			Used:     5061.0,
		},
		{
			Type:     metrics.CPURequestQuota,
			Capacity: 2663.0,
			Used:     2397.0,
		},
		{
			Type:     metrics.MemoryLimitQuota,
			Capacity: 2097152.0,
			Used:     1945600.0,
		},
		{
			Type:     metrics.MemoryRequestQuota,
			Capacity: 1048576.0,
			Used:     921600.0,
		},
	}

	// Create testKubeController1 with allocation resources
	testKubeController1 = createKubeController(testClusterName, testNamespace, "controller1", util.KindDeployment,
		"controller1-UID", testAllocationResources)
	testKubeController2 = repository.NewKubeController(testClusterName, testNamespace, "controller2", testCustomControllerType, "controller2-UID")

	testWorkloadControllerDTOBuilder = NewWorkloadControllerDTOBuilder(
		map[string]*repository.KubeController{
			"controller1-UID": testKubeController1,
			"controller2-UID": testKubeController2,
		},
		map[string]string{
			testNamespace: testNamespaceUID,
		})
)

func TestBuildDTOs(t *testing.T) {
	entityDTOs, _ := testWorkloadControllerDTOBuilder.BuildDTOs()
	for _, entityDTO := range entityDTOs {
		if entityDTO.GetId() == "controller1-UID" {
			// cloneable and suspendable is false for WorkloadController
			actionEligibility := entityDTO.GetActionEligibility()
			assert.False(t, actionEligibility.GetCloneable())
			assert.False(t, actionEligibility.GetSuspendable())

			// Test commodity sold DTOs
			expectedCommoditiesSold := createCommoditiesSold(testKubeController1.UID)
			commoditiesSold := entityDTO.GetCommoditiesSold()
			assert.ElementsMatch(t, expectedCommoditiesSold, commoditiesSold)

			// Test commodity bought DTOs
			expectedCommoditiesBought := createCommoditiesBought(testNamespaceUID)
			commoditiesBought := entityDTO.GetCommoditiesBought()[0]
			assert.Equal(t, proto.EntityDTO_NAMESPACE, commoditiesBought.GetProviderType())
			assert.Equal(t, testNamespaceUID, commoditiesBought.GetProviderId())
			assert.ElementsMatch(t, expectedCommoditiesBought, commoditiesBought.GetBought())

			// Test create WorkloadControllerData with DeploymentData
			expectedWorkloadControllerData1 := &proto.EntityDTO_WorkloadControllerData{
				ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
					DeploymentData: &proto.EntityDTO_DeploymentData{},
				},
			}
			workloadControllerData1 := entityDTO.GetWorkloadControllerData()
			assert.EqualValues(t, expectedWorkloadControllerData1, workloadControllerData1)
		} else if entityDTO.GetId() == "controller2-UID" {
			// cloneable and suspendable is false for WorkloadController
			actionEligibility := entityDTO.GetActionEligibility()
			assert.False(t, actionEligibility.GetCloneable())
			assert.False(t, actionEligibility.GetSuspendable())

			// Test create WorkloadControllerData with CustomControllerData
			expectedWorkloadControllerData2 := &proto.EntityDTO_WorkloadControllerData{
				ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
					CustomControllerData: &proto.EntityDTO_CustomControllerData{
						CustomControllerType: &testCustomControllerType,
					},
				},
			}
			workloadControllerData2 := entityDTO.GetWorkloadControllerData()
			assert.EqualValues(t, expectedWorkloadControllerData2, workloadControllerData2)
		}
	}
}

func createKubeController(clustername, namespace, name, controllerType, uid string,
	testAllocationResources []*repository.KubeDiscoveredResource) *repository.KubeController {
	kubeController := repository.NewKubeController(clustername, namespace, name, controllerType, uid)
	for _, allocationResource := range testAllocationResources {
		kubeController.AddAllocationResource(allocationResource.Type, allocationResource.Capacity, allocationResource.Used)
	}
	return kubeController
}

func createCommoditiesSold(key string) []*proto.CommodityDTO {
	var commoditiesSold []*proto.CommodityDTO
	for _, resource := range testAllocationResources {
		commodityType, _ := rTypeMapping[resource.Type]
		commodityDTO := &proto.CommodityDTO{
			CommodityType: &commodityType,
			Key:           &key,
			Used:          &resource.Used,
			Peak:          &resource.Used,
			Capacity:      &resource.Capacity,
			Resizable:     &testResizable,
		}
		commoditiesSold = append(commoditiesSold, commodityDTO)
	}
	return commoditiesSold
}

func createCommoditiesBought(key string) []*proto.CommodityDTO {
	var commoditiesBought []*proto.CommodityDTO
	for _, resource := range testAllocationResources {
		commodityType, _ := rTypeMapping[resource.Type]
		commodityDTO := &proto.CommodityDTO{
			CommodityType: &commodityType,
			Key:           &key,
			Used:          &resource.Used,
			Peak:          &resource.Used,
			Resizable:     &testResizable,
		}
		commoditiesBought = append(commoditiesBought, commodityDTO)
	}
	return commoditiesBought
}
