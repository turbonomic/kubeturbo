package dtofactory

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"time"
)

var (
	ContainerSpecCommoditiesSold = []proto.CommodityDTO_CommodityType{
		proto.CommodityDTO_VCPU,
		proto.CommodityDTO_VMEM,
	}
)

type containerSpecDTOBuilder struct {
	// Map from ContainerSpec ID to ContainerSpec entity which contains list of individual container replica commodities
	// data to be aggregated
	containerSpecMap map[string]*repository.ContainerSpec
	// Aggregator to aggregate container replicas commodity utilization data
	containerUtilizationDataAggregator aggregation.ContainerUtilizationDataAggregator
	// Aggregator to aggregate container replicas commodity usage data (used, peak and capacity)
	containerUsageDataAggregator aggregation.ContainerUsageDataAggregator
}

func NewContainerSpecDTOBuilder(containerSpecMap map[string]*repository.ContainerSpec,
	containerUtilizationDataAggregator aggregation.ContainerUtilizationDataAggregator,
	containerUsageDataAggregator aggregation.ContainerUsageDataAggregator) *containerSpecDTOBuilder {
	return &containerSpecDTOBuilder{
		containerSpecMap:                   containerSpecMap,
		containerUtilizationDataAggregator: containerUtilizationDataAggregator,
		containerUsageDataAggregator:       containerUsageDataAggregator,
	}
}

func (builder *containerSpecDTOBuilder) BuildDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for containerSpecId, containerSpec := range builder.containerSpecMap {
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
func (builder *containerSpecDTOBuilder) getCommoditiesSold(containerSpec *repository.ContainerSpec) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	for _, commodityType := range ContainerSpecCommoditiesSold {
		// commodities is a list of commodity DTOs of commodityType sold by container replicas represented by this
		// ContainerSpec entity
		commodities, exists := containerSpec.ContainerCommodities[commodityType]
		if !exists {
			glog.Errorf("ContainerSpec %s has no %s commodity from the collected ContainerSpec",
				containerSpec.ContainerSpecId, commodityType)
			continue
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)

		// Aggregate container replicas utilization data
		utilizationDataPoints, err := builder.containerUtilizationDataAggregator.Aggregate(commodities)
		if err != nil {
			glog.Errorf("Error aggregating commodity utilization data for ContainerSpec %s, %v",
				containerSpec.ContainerSpecId, err)
			continue
		}
		currentMs := time.Now().UnixNano() / int64(time.Millisecond)
		// Currently we only collect one set of metrics data points from Kubernetes within a discovery cycle so the
		// lastPointTimestampMs of utilizationData is current timestamp in milliseconds and intervalMs is 0.
		commSoldBuilder.UtilizationData(utilizationDataPoints, currentMs, 0)

		// Aggregate container replicas usage data (capacity, used and peak)
		aggregatedCap, aggregatedUsed, aggregatedPeak, err :=
			builder.containerUsageDataAggregator.Aggregate(commodities)
		if err != nil {
			glog.Errorf("Error aggregating commodity usage data for ContainerSpec %s, %v",
				containerSpec.ContainerSpecId, err)
			continue
		}
		commSoldBuilder.Capacity(aggregatedCap)
		commSoldBuilder.Peak(aggregatedPeak)
		commSoldBuilder.Used(aggregatedUsed)

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
