package dtofactory

import (
	"math"
	"sort"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	ContainerSpecResourceTypes = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
		metrics.VCPUThrottling,
	}
)

type containerSpecDTOBuilder struct {
	// Map from ContainerSpec ID to ContainerSpecMetrics which contains list of container replicas usage metrics data to be aggregated
	containerSpecMetricsMap map[string]*repository.ContainerSpecMetrics
	// Aggregator to aggregate container replicas commodity utilization data
	containerUtilizationDataAggregator aggregation.ContainerUtilizationDataAggregator
	// Aggregator to aggregate container replicas commodity usage data (used, peak and capacity)
	containerUsageDataAggregator aggregation.ContainerUsageDataAggregator
	// Cluster Summary needed to populate the labels and annotations from the workload controller cache
	clusterSummary *repository.ClusterSummary
}

func NewContainerSpecDTOBuilder(clusterSummary *repository.ClusterSummary, containerSpecMetricsMap map[string]*repository.ContainerSpecMetrics,
	containerUtilizationDataAggregator aggregation.ContainerUtilizationDataAggregator,
	containerUsageDataAggregator aggregation.ContainerUsageDataAggregator) *containerSpecDTOBuilder {
	return &containerSpecDTOBuilder{
		clusterSummary:                     clusterSummary,
		containerSpecMetricsMap:            containerSpecMetricsMap,
		containerUtilizationDataAggregator: containerUtilizationDataAggregator,
		containerUsageDataAggregator:       containerUsageDataAggregator,
	}
}

func (builder *containerSpecDTOBuilder) BuildDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for containerSpecId, containerSpec := range builder.containerSpecMetricsMap {
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_SPEC, containerSpecId)
		entityDTOBuilder.DisplayName(containerSpec.ContainerSpecName)
		commoditiesSold, err := builder.getCommoditiesSold(containerSpec)
		if err != nil {
			glog.Errorf("Error creating commodities sold by ContainerSpec %s, %v", containerSpecId, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)
		// ContainerSpec entity is not monitored and will not be sent to Market analysis engine in turbo server
		entityDTOBuilder.Monitored(false)
		if builder.clusterSummary != nil {
			var labelAnnotations []map[string]string

			controller, found := builder.clusterSummary.ControllerMap[containerSpec.ControllerUID]
			if found {
				labelAnnotations = append(labelAnnotations, controller.Labels, controller.Annotations)
				entityDTOBuilder.WithProperties(property.BuildLabelAnnotationProperties(labelAnnotations))

			}
		}
		dto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build ContainerSpec[%s] entityDTO: %v", containerSpecId, err)
			continue
		}
		result = append(result, dto)
	}
	return result, nil
}

// getCommoditiesSold gets commodity DTOs with aggregated container utilization and usage data.
func (builder *containerSpecDTOBuilder) getCommoditiesSold(containerSpecMetrics *repository.ContainerSpecMetrics) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	for _, resourceType := range ContainerSpecResourceTypes {
		commodityType, exist := rTypeMapping[resourceType]
		if !exist {
			glog.Errorf("Unsupported resource type %s when building commoditiesSold for ContainerSpec %s",
				resourceType, containerSpecMetrics.ContainerSpecId)
			continue
		}
		resourceMetrics, exists := containerSpecMetrics.ContainerMetrics[resourceType]
		if !exists {
			glog.V(4).Infof("ContainerMetrics collected from ContainerSpec %s has no %s resource type",
				containerSpecMetrics.ContainerSpecId, resourceType)
			continue
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)

		// Aggregate container replicas utilization data.
		// Note that the returned dataIntervalMs is not the real sampling interval because there could be multiple data
		// points from container replicas discovered at the same time in one set of data samples. This is just a calculated
		// equivalent interval between 2 data points to be fed into percentile based algorithm in Turbo server side.
		// We don't need utilization data for VCPU Throttling and average aggregation strategies are not applicable.
		if resourceType != metrics.VCPUThrottling {
			utilizationDataPoints, lastPointTimestamp, dataIntervalMs, err := builder.containerUtilizationDataAggregator.Aggregate(resourceMetrics)
			if err != nil {
				glog.Errorf("Error aggregating %s utilization data for ContainerSpec %s, %v",
					resourceType, containerSpecMetrics.ContainerSpecId, err)
				continue
			}
			// Construct UtilizationData with multiple data points, last point timestamp in milliseconds and interval in milliseconds
			commSoldBuilder.UtilizationData(utilizationDataPoints, lastPointTimestamp, dataIntervalMs)

			// Aggregate container replicas usage data (capacity, used and peak)
			aggregatedCap, aggregatedUsed, aggregatedPeak, err :=
				builder.containerUsageDataAggregator.Aggregate(resourceMetrics)
			if err != nil {
				glog.Errorf("Error aggregating commodity usage data for ContainerSpec %s, %v",
					containerSpecMetrics.ContainerSpecId, err)
				continue
			}
			commSoldBuilder.Capacity(aggregatedCap)
			commSoldBuilder.Peak(aggregatedPeak)
			commSoldBuilder.Used(aggregatedUsed)
		} else {
			aggregatedCap, aggregatedUsed, aggregatedPeak, ok := aggregateThrottlingSamples(resourceMetrics.Used)
			if ok {
				commSoldBuilder.Capacity(aggregatedCap)
				commSoldBuilder.Peak(aggregatedPeak)
				commSoldBuilder.Used(aggregatedUsed)
			}
		}

		// Commodities sold by ContainerSpec entities have resizable flag as true so as to update resizable flag to
		// the commodities sold by corresponding Container entities in the server side when taking historical percentile
		// utilization data into consideration for resizing.
		commSoldBuilder.Resizable(true)

		// Commodities sold by ContainerSpec entities are active so that they can be stored in database in Turbo server.
		commSoldBuilder.Active(true)
		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build commodity sold %s: %v", commodityType, err)
			continue
		}
		commoditiesSold = append(commoditiesSold, commSold)
	}
	return commoditiesSold, nil
}

func aggregateThrottlingSamples(samples interface{}) (float64, float64, float64, bool) {
	typedSamples, ok := samples.([][]metrics.ThrottlingCumulative)
	if !ok {
		return 0, 0, 0, false
	}

	var throttledOverall, totalOverall, peakOverall float64
	for _, singleContainerSamples := range typedSamples {
		numberOfSamples := len(singleContainerSamples)
		if numberOfSamples <= 1 {
			// We don't have enough samples to calculate this value.
			continue
		}
		sort.SliceStable(singleContainerSamples, func(i, j int) bool {
			return singleContainerSamples[i].Timestamp < singleContainerSamples[j].Timestamp
		})

		throttledOverall += singleContainerSamples[numberOfSamples-1].Throttled - singleContainerSamples[0].Throttled
		totalOverall += singleContainerSamples[numberOfSamples-1].Total - singleContainerSamples[0].Total

		var peak float64
		for i := 0; i < numberOfSamples-1; i++ {
			total := singleContainerSamples[i+1].Total - singleContainerSamples[i].Total
			singleSamplePercent := float64(0)
			if total > 0 {
				singleSamplePercent = (singleContainerSamples[i+1].Throttled - singleContainerSamples[i].Throttled) * 100 / total
			}
			peak = math.Max(peak, singleSamplePercent)
		}
		peakOverall = math.Max(peakOverall, peak)
	}
	avgThrottled := float64(0)
	if totalOverall > 0 {
		avgThrottled = throttledOverall * 100 / totalOverall
	}

	return 100, avgThrottled, peakOverall, true
}
