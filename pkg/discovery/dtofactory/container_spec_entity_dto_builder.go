package dtofactory

import (
	"math"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.com/turbonomic/kubeturbo/pkg/features"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
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
			controller, found := builder.clusterSummary.ControllerMap[containerSpec.ControllerUID]
			if found {
				entityDTOBuilder.WithProperties(property.BuildLabelAnnotationProperties(controller.Labels, controller.Annotations, detectors.AWContainerSpec))
			}
		}
		//container spec data
		cPUThrottlingType := proto.EntityDTO_timeBased
		containerSpecData := &proto.EntityDTO_ContainerSpecData{
			CpuThrottlingType: &cPUThrottlingType,
		}
		entityDTOBuilder.ContainerSpecData(containerSpecData)

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
		if resourceType == metrics.VCPUThrottling && !utilfeature.DefaultFeatureGate.Enabled(features.ThrottlingMetrics) {
			continue
		}
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

		if resourceType == metrics.VCPUThrottling {
			// We don't need utilization data for VCPU Throttling and average aggregation strategies are not applicable.
			typedUsed, ok := resourceMetrics.Used.([][]metrics.ThrottlingCumulative)
			if ok {
				containerCPUMetrics, exists := containerSpecMetrics.ContainerMetrics[metrics.CPU]
				if !exists {
					glog.Warningf("CPU metrics is missing in resource metrics for ContainerSpec %s", containerSpecMetrics.ContainerSpecId)
					continue
				}
				// Get VCPU capacity for containerSpec from container CPU metrics.
				containerSpecVCPUCapacity := aggregation.GetResourceCapacity(containerCPUMetrics)
				aggregatedCap, aggregatedUsed, aggregatedPeak :=
					aggregateThrottlingSamples(containerSpecMetrics.ContainerSpecId, containerSpecVCPUCapacity, typedUsed)
				commSoldBuilder.Capacity(aggregatedCap)
				commSoldBuilder.Peak(aggregatedPeak)
				commSoldBuilder.Used(aggregatedUsed)
			} else {
				glog.Warningf("Invalid throttling metrics type: expected: [][]metrics.ThrottlingCumulative, got: %T.", resourceMetrics.Used)
			}
		} else {
			// Aggregate container replicas utilization data.
			// Note that the returned dataIntervalMs is not the real sampling interval because there could be multiple data
			// points from container replicas discovered at the same time in one set of data samples. This is just a calculated
			// equivalent interval between 2 data points to be fed into percentile based algorithm in Turbo server side.
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

// aggregateThrottlingSamples aggregates the throttling samples collected across all containers for the
// given container spec.
// Throttled values is the percentage of overall throttled counter sum wrt the overall total counter sum
// across all containers. Peak is the peak of peaks, ie. the peak of individual container peaks calculated
// from diff of subsequent samples per container.
func aggregateThrottlingSamples(containerSpecId string, containerSpecVCPUCapacity float64, samples [][]metrics.ThrottlingCumulative) (float64, float64, float64) {
	var throttledTimeOverall, totalUsageOverall, peakThrottledPercentOverall float64
	for _, singleContainerSamples := range samples {
		// Include container samples only if corresponding CPU limit is same as containerSpec VCPU capacity.
		filteredContainerSamples := filterContainerThrottlingSamples(containerSpecId, singleContainerSamples, containerSpecVCPUCapacity)
		containerThrottledTime, containerTotalUsage, containerThrottledPercentPeak, ok :=
			aggregateContainerThrottlingSamples("", filteredContainerSamples)
		if !ok {
			// We don't have enough samples to calculate this value.
			continue
		}
		throttledTimeOverall += containerThrottledTime
		totalUsageOverall += containerTotalUsage
		peakThrottledPercentOverall = math.Max(peakThrottledPercentOverall, containerThrottledPercentPeak)
	}

	avgThrottledTimeOverall := float64(0)
	if throttledTimeOverall > 0 || totalUsageOverall > 0 {
		avgThrottledTimeOverall = throttledTimeOverall * 100 / (throttledTimeOverall + totalUsageOverall)
	}

	return 100, avgThrottledTimeOverall, peakThrottledPercentOverall
}

func filterContainerThrottlingSamples(containerSpecId string, singleContainerSamples []metrics.ThrottlingCumulative, containerSpecVCPUCapacity float64) []metrics.ThrottlingCumulative {
	var filteredSamples []metrics.ThrottlingCumulative
	for _, sample := range singleContainerSamples {
		if int(math.Round(sample.CPULimit)) == int(math.Round(containerSpecVCPUCapacity)) {
			filteredSamples = append(filteredSamples, sample)
		} else if sample.CPULimit != 0 {
			// Log a message only when CPU limit of container sample is not 0, which means CPU limit is defined for corresponding
			// container.
			// When CPU limit is not defined on a container, VCPU capacity is node VCPU capacity. We don't need to log
			// such valid case.
			glog.V(3).Infof("Container data sample with CPU limits %v collected at timestamp %v doesn't match VCPU capacity %v for ContainerSpec %s. Skip this data sample.",
				sample.CPULimit, sample.Timestamp, containerSpecVCPUCapacity, containerSpecId)
		}
	}
	return filteredSamples
}
