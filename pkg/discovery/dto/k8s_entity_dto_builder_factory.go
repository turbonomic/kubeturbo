package dto

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
)

var (
	// VCPU, VMEM, MEM_PROVISIONED, CPU_PROVISIONED
	rTypeMapping = map[metrics.ResourceType]proto.CommodityDTO_CommodityType{
		metrics.CPU:               proto.CommodityDTO_VCPU,
		metrics.Memory:            proto.CommodityDTO_VMEM,
		metrics.CPUProvisioned:    proto.CommodityDTO_CPU_PROVISIONED,
		metrics.MemoryProvisioned: proto.CommodityDTO_MEM_PROVISIONED,
	}
)

type K8sEntityDTOBuilderFactory struct {
}

type k8sEntityDTOBuilder interface {
	buildEntityDTOs() *proto.EntityDTO
}

type generalBuilder struct {
	metricsSink *metrics.EntityMetricSink
}

func (builder *generalBuilder) getResourceCommoditiesSold(entityType task.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for _, rType := range resourceTypesList {
		cType, exist := rTypeMapping[rType]
		if !exist {
			glog.Errorf("Commodity type %s sold by %s is not supported", rType, entityType)
		}

		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := builder.metricsSink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s", rType, entityType, entityID)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)

		capacityUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Capacity)
		capacityMetric, err := builder.metricsSink.GetMetric(capacityUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s capacity for %s %s", rType, entityType, entityID)
			continue
		}
		capacityValue := capacityMetric.GetValue().(float64)

		commSold, err := sdkbuilder.NewCommodityDTOBuilder(cType).
			Capacity(capacityValue).
			Used(usedValue).
			Create()
		if err != nil {
			// TODO return?
			return nil, err
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder *generalBuilder) getResourceCommoditiesBought(entityType task.DiscoveredEntityType, entityID string,
	rTypesMapping map[metrics.ResourceType]proto.CommodityDTO_CommodityType) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for rType, cType := range rTypesMapping {
		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := builder.metricsSink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s", rType, entityType, entityID)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)

		commSold, err := sdkbuilder.NewCommodityDTOBuilder(cType).
			Used(usedValue).
			Create()
		if err != nil {
			// TODO return?
			return nil, err
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}
