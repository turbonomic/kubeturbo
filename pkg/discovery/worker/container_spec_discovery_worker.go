package worker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	agg "github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Converts ContainerSpecMetrics objects to ContainerSpec entity DTOs
type k8sContainerSpecDiscoveryWorker struct {
	commodityConfig *dtofactory.CommodityConfig
}

func NewK8sContainerSpecDiscoveryWorker(commodityConfig *dtofactory.CommodityConfig) *k8sContainerSpecDiscoveryWorker {
	return &k8sContainerSpecDiscoveryWorker{
		commodityConfig: commodityConfig,
	}
}

// ContainerSpec discovery worker collects ContainerSpecMetrics discovered by different discovery workers.
// It merges multiple resource used data of container replicas belonging to the same ContainerSpec but discovered by
// different discovery workers, and aggregates container utilization and usage data based on the specified aggregation
// strategies.
// Then it creates entity DTOs for the ContainerSpecs with the aggregated commodities data of container replicas to be
// sent to the Turbonomic server.
func (worker *k8sContainerSpecDiscoveryWorker) Do(clusterSummary *repository.ClusterSummary, containerSpecMetrics []*repository.ContainerSpecMetrics,
	utilizationDataAggStrategy, usageDataAggStrategy string) ([]*proto.EntityDTO, error) {
	// Get data aggregators based on the given data aggregation strategies
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators(utilizationDataAggStrategy, usageDataAggStrategy)
	// Create containerSpecMetrics map from ContainerSpec ID to ContainerSpecMetrics with merged resource usage data samples
	// of container replicas
	containerSpecMetricsMap := worker.createContainerSpecMetricsMap(containerSpecMetrics)
	containerSpecEntityDTOBuilder := dtofactory.NewContainerSpecDTOBuilder(clusterSummary, containerSpecMetricsMap, utilizationDataAggregator,
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

// createContainerSpecMetricsMap creates map from ContainerSpec ID to ContainerSpecMetrics object with merged resource usage
// data samples of container replicas.
func (worker *k8sContainerSpecDiscoveryWorker) createContainerSpecMetricsMap(containerSpecMetricsList []*repository.ContainerSpecMetrics) map[string]*repository.ContainerSpecMetrics {
	// Map from ContainerSpec ID to combined ContainerSpecMetrics
	containerSpecMetricsMap := make(map[string]*repository.ContainerSpecMetrics)
	for _, containerSpecMetrics := range containerSpecMetricsList {
		containerSpecId := containerSpecMetrics.ContainerSpecId
		containerSpecMetricsInMap, exists := containerSpecMetricsMap[containerSpecId]
		if !exists {
			containerSpecMetricsMap[containerSpecId] = containerSpecMetrics
		} else {
			for resourceType, existingResourceMetrics := range containerSpecMetricsInMap.ContainerMetrics {
				containerMetrics, exists := containerSpecMetrics.ContainerMetrics[resourceType]
				if !exists {
					glog.Errorf("%s resource does not exist for ContainerSpec %s", resourceType, containerSpecMetrics.ContainerSpecId)
					continue
				}
				// Append resource capacity values of same resource type of container replicas from different nodes.
				// To make sure CPU capacity of container spec is consistent, we collect capacity values from all container
				// replicas here and will use max value when building container spec entity dto.
				existingResourceMetrics.Capacity = append(existingResourceMetrics.Capacity, containerMetrics.Capacity...)
				// Append containerMetrics used data points of same resource type of container replicas from different nodes.
				existingResourceMetrics.Used = appendMetricsByType(existingResourceMetrics.Used, containerMetrics.Used)
			}
			// Increment number of container replicas
			containerSpecMetricsInMap.ContainerReplicas++
		}
	}
	for containerSpecId, containerSpec := range containerSpecMetricsMap {
		glog.V(4).Infof("Discovered ContainerSpec entity %s has %d container replicas", containerSpecId, containerSpec.ContainerReplicas)
	}
	return containerSpecMetricsMap
}

func appendMetricsByType(slice, elements interface{}) interface{} {
	switch typedSlice := slice.(type) {
	case []metrics.Point:
		typedElements, isRightType := elements.([]metrics.Point)
		if isRightType {
			typedSlice = append(typedSlice, typedElements...)
		}
		return typedSlice
	case [][]metrics.ThrottlingCumulative:
		typedElements, isRightType := elements.([][]metrics.ThrottlingCumulative)
		if isRightType {
			typedSlice = append(typedSlice, typedElements...)
		}
		return typedSlice
	}
	return nil
}
