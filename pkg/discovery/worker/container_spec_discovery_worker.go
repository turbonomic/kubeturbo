package worker

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	agg "github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Converts K8s ContainerSpec objects to entity DTOs
type k8sContainerSpecDiscoveryWorker struct{}

func NewK8sContainerSpecDiscoveryWorker() *k8sContainerSpecDiscoveryWorker {
	return &k8sContainerSpecDiscoveryWorker{}
}

// ContainerSpec discovery worker collects ContainerSpecs discovered by different discovery workers.
// It merges the commodities data of container replicas belonging to the same ContainerSpec but discovered by different
// discovery workers, and aggregates container utilization and usage data based on the specified aggregation strategies.
// Then it creates entity DTOs for the ContainerSpecs with the aggregated commodities data of container replicas to be
// sent to the Turbonomic server.
func (worker *k8sContainerSpecDiscoveryWorker) Do(containerSpecs []*repository.ContainerSpec,
	utilizationDataAggStrategy, usageDataAggStrategy string) ([]*proto.EntityDTO, error) {
	// Get data aggregators based on the given data aggregation strategies
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators(utilizationDataAggStrategy, usageDataAggStrategy)
	// Create containerSpecs map from ContainerSpec ID to ContainerSpec with merge commodities data of container replicas
	containerSpecMap := worker.createContainerSpecMap(containerSpecs)
	containerSpecEntityDTOBuilder := dtofactory.NewContainerSpecDTOBuilder(containerSpecMap, utilizationDataAggregator,
		usageDataAggregator)
	containerSpecEntityDTOs, err := containerSpecEntityDTOBuilder.BuildDTOs()
	if err != nil {
		return nil, fmt.Errorf("error while creating ContainerSpec entityDTOs: %v", err)
	}
	return containerSpecEntityDTOs, nil
}

// getContainerDataAggregators returns utilization and usage data aggregators based on the given utilization and usage
// data aggregation strategies. If given data aggregation strategies are not supported, use default strategies.
func (worker *k8sContainerSpecDiscoveryWorker) getContainerDataAggregators(utilizationDataAggStrategy,
	usageDataAggStrategy string) (agg.ContainerUtilizationDataAggregator, agg.ContainerUsageDataAggregator) {
	utilizationDataAggregator, exists := agg.ContainerUtilizationDataAggregators[utilizationDataAggStrategy]
	if !exists {
		glog.Errorf("Container utilization data aggregation strategy '%s' is not supported. Use default '%s' strategy",
			utilizationDataAggStrategy, agg.DefaultContainerUtilizationDataAggStrategy)
		utilizationDataAggregator = agg.ContainerUtilizationDataAggregators[agg.DefaultContainerUtilizationDataAggStrategy]
	}
	glog.Infof("ContainerSpec will aggregate Containers utilization data by '%s'", utilizationDataAggregator)

	usageDataAggregator, exists := agg.ContainerUsageDataAggregators[usageDataAggStrategy]
	if !exists {
		glog.Errorf("Container usage data aggregation strategy '%s' is not supported. Use default '%s' strategy",
			usageDataAggStrategy, agg.DefaultContainerUsageDataAggStrategy)
		usageDataAggregator = agg.ContainerUsageDataAggregators[agg.DefaultContainerUsageDataAggStrategy]
	}
	glog.Infof("ContainerSpec will aggregate Containers usage data by '%s'", usageDataAggregator)
	return utilizationDataAggregator, usageDataAggregator
}

// createContainerSpecMap creates map from ContainerSpec ID to ContainerSpec entity with merged VCPU and VMem commodity
// DTOs of container replicas.
func (worker *k8sContainerSpecDiscoveryWorker) createContainerSpecMap(containerSpecList []*repository.ContainerSpec) map[string]*repository.ContainerSpec {
	// Map from ContainerSpec ID to ContainerSpec object
	containerSpecMap := make(map[string]*repository.ContainerSpec)
	for _, containerSpec := range containerSpecList {
		containerSpecId := containerSpec.ContainerSpecId
		existingContainerSpec, exists := containerSpecMap[containerSpecId]
		if !exists {
			containerSpecMap[containerSpecId] = containerSpec
		} else {
			// Append commodity DTOs of the same commodity type of container replicas
			for commodityType, existingCommodities := range existingContainerSpec.ContainerCommodities {
				commodities, exists := containerSpec.ContainerCommodities[commodityType]
				if !exists {
					glog.Errorf("%s commodities do not exist for ContainerSpec %s", commodityType, containerSpec.ContainerSpecId)
					continue
				}
				existingCommodities = append(existingCommodities, commodities...)
				existingContainerSpec.ContainerCommodities[commodityType] = existingCommodities
			}
			// Increment number of container replicas
			existingContainerSpec.ContainerReplicas++
		}
	}
	for containerSpecId, containerSpec := range containerSpecMap {
		glog.V(4).Infof("Discovered ContainerSpec entity %s has %d container replicas", containerSpecId, containerSpec.ContainerReplicas)
	}
	return containerSpecMap
}
