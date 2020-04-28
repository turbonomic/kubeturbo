package worker

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	agg "github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"reflect"
	"testing"
)

func Test_k8sContainerSpecDiscoveryWorker_getContainerDataAggregator(t *testing.T) {
	worker := &k8sContainerSpecDiscoveryWorker{}
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators("allUtilizationData", "maxUsageData")
	assert.Equal(t, agg.ContainerUtilizationDataAggregators["allUtilizationData"], utilizationDataAggregator)
	assert.Equal(t, agg.ContainerUsageDataAggregators["maxUsageData"], usageDataAggregator)
}

func Test_k8sContainerSpecDiscoveryWorker_getContainerDataAggregator_defaultStrategy(t *testing.T) {
	worker := &k8sContainerSpecDiscoveryWorker{}
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators("testUtilizationDataStrategy", "testUsageDataStrategy")
	// If input utilizationDataAggStrategy and usageDataAggStrategy are not support, use default data aggregators
	assert.Equal(t, agg.ContainerUtilizationDataAggregators[agg.DefaultContainerUtilizationDataAggStrategy], utilizationDataAggregator)
	assert.Equal(t, agg.ContainerUsageDataAggregators[agg.DefaultContainerUsageDataAggStrategy], usageDataAggregator)
}

func Test_k8sContainerSpecDiscoveryWorker_createContainerSpecMap(t *testing.T) {
	namespace := "namespace"
	controllerUID := "controllerUID"
	containerSpecName := "containerSpecName"
	containerSpecId := "containerSpecId"
	cpuCommType := proto.CommodityDTO_VCPU
	memCommType := proto.CommodityDTO_VMEM
	cpuComm1 := createCommodityDTO(cpuCommType, 1.0, 1.0, 2.0)
	memComm1 := createCommodityDTO(memCommType, 1.0, 1.0, 2.0)
	cpuComm2 := createCommodityDTO(cpuCommType, 2.0, 2.0, 3.0)
	memComm2 := createCommodityDTO(memCommType, 2.0, 2.0, 3.0)

	// containerSpec1 and containerSpec2 collect of the same ContainerSpec entity from 2 container replicas
	containerSpec1 := &repository.ContainerSpec{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 1,
		ContainerCommodities: map[proto.CommodityDTO_CommodityType][]*proto.CommodityDTO{
			cpuCommType: {
				cpuComm1,
			},
			memCommType: {
				memComm1,
			},
		},
	}
	containerSpec2 := &repository.ContainerSpec{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 1,
		ContainerCommodities: map[proto.CommodityDTO_CommodityType][]*proto.CommodityDTO{
			cpuCommType: {
				cpuComm2,
			},
			memCommType: {
				memComm2,
			},
		},
	}

	expectedContainerSpec := repository.ContainerSpec{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 2,
		ContainerCommodities: map[proto.CommodityDTO_CommodityType][]*proto.CommodityDTO{
			cpuCommType: {
				cpuComm1,
				cpuComm2,
			},
			memCommType: {
				memComm1,
				memComm2,
			},
		},
	}

	worker := &k8sContainerSpecDiscoveryWorker{}
	containerSpecMap := worker.createContainerSpecMap([]*repository.ContainerSpec{containerSpec1, containerSpec2})
	containerSpec := *containerSpecMap[containerSpecId]
	if !reflect.DeepEqual(expectedContainerSpec, containerSpec) {
		t.Errorf("Test case failed: createContainerSpecMap:\nexpected:\n%++v\nactual:\n%++v",
			expectedContainerSpec, containerSpec)
	}
}

func createCommodityDTO(commodityType proto.CommodityDTO_CommodityType, used, peak, capacity float64) *proto.CommodityDTO {
	return &proto.CommodityDTO{
		CommodityType: &commodityType,
		Used:          &used,
		Peak:          &peak,
		Capacity:      &capacity,
	}
}
