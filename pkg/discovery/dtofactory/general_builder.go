package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

var (
	// This map maps resource type to commodity types defined in ProtoBuf.
	// VCPU, VMEM, MEM_PROVISIONED, CPU_PROVISIONED
	rTypeMapping = map[metrics.ResourceType]proto.CommodityDTO_CommodityType{
		metrics.CPU:               proto.CommodityDTO_VCPU,
		metrics.Memory:            proto.CommodityDTO_VMEM,
		metrics.CPUProvisioned:    proto.CommodityDTO_CPU_PROVISIONED,
		metrics.MemoryProvisioned: proto.CommodityDTO_MEM_PROVISIONED,
		metrics.Transaction:       proto.CommodityDTO_TRANSACTION,
		metrics.CPULimit:          proto.CommodityDTO_CPU_ALLOCATION,
		metrics.MemoryLimit:       proto.CommodityDTO_MEM_ALLOCATION,
	}
)

type ValueConversionFunc func(input float64) float64

type converter struct {
	valueConverters map[metrics.ResourceType]ValueConversionFunc
}

func NewConverter() *converter {
	return &converter{
		valueConverters: make(map[metrics.ResourceType]ValueConversionFunc),
	}
}

func (c *converter) Set(cFunc ValueConversionFunc, rTypes ...metrics.ResourceType) *converter {
	for _, rType := range rTypes {
		c.valueConverters[rType] = cFunc
	}
	return c
}

func (c *converter) Convertible(rType metrics.ResourceType) bool {
	_, exist := c.valueConverters[rType]
	return exist
}

// must call convertible before calling convert.
func (c *converter) Convert(rType metrics.ResourceType, value float64) float64 {
	return c.valueConverters[rType](value)
}

type commodityAttrSetter func(commBuilder *sdkbuilder.CommodityDTOBuilder)

type attributeSetter struct {
	attrSetterFunc map[metrics.ResourceType][]commodityAttrSetter
}

func NewCommodityAttrSetter() *attributeSetter {
	return &attributeSetter{
		attrSetterFunc: make(map[metrics.ResourceType][]commodityAttrSetter),
	}
}

func (s *attributeSetter) Add(setFunc commodityAttrSetter, rTypes ...metrics.ResourceType) *attributeSetter {
	for _, rType := range rTypes {

		funcs, exist := s.attrSetterFunc[rType]
		if !exist {
			funcs = []commodityAttrSetter{}
		}
		funcs = append(funcs, setFunc)
		s.attrSetterFunc[rType] = funcs
	}
	return s
}

func (s *attributeSetter) Settable(rType metrics.ResourceType) bool {
	_, exist := s.attrSetterFunc[rType]
	return exist
}

func (s *attributeSetter) Set(rType metrics.ResourceType, commBuilder *sdkbuilder.CommodityDTOBuilder) {
	for _, setFunc := range s.attrSetterFunc[rType] {
		setFunc(commBuilder)
	}
}

type generalBuilder struct {
	metricsSink *metrics.EntityMetricSink
}

func newGeneralBuilder(sink *metrics.EntityMetricSink) generalBuilder {
	return generalBuilder{
		metricsSink: sink,
	}
}

// TODO cpuFrequency is passed in as a parameter. We need special handling for cpu related metric as the value collected by Kubernetes is in number of cores. We need to convert it to MHz.
func (builder generalBuilder) getResourceCommoditiesSold(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType, converter *converter, commodityAttrSetter *attributeSetter) ([]*proto.CommodityDTO, error) {

	var resourceCommoditiesSold []*proto.CommodityDTO
	for _, rType := range resourceTypesList {
		cType, exist := rTypeMapping[rType]
		if !exist {
			glog.Errorf("Commodity type %s sold by %s is not supported", rType, entityType)
			continue
		}
		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)

		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := builder.metricsSink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s: %s", rType, entityType, entityID, err)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)
		if converter != nil && converter.Convertible(rType) {
			oldValue := usedValue
			usedValue = converter.Convert(rType, usedValue)
			glog.V(4).Infof("Convert %s used value from %f to %f for %s - %s", rType, oldValue, usedValue, entityType, entityID)
		}
		commBoughtBuilder.Used(usedValue)

		capacityUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Capacity)
		capacityMetric, err := builder.metricsSink.GetMetric(capacityUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s capacity for %s %s: %s", rType, entityType, entityID, err)
			continue
		}
		capacityValue := capacityMetric.GetValue().(float64)
		if converter != nil && converter.Convertible(rType) {
			oldValue := capacityValue
			capacityValue = converter.Convert(rType, capacityValue)
			glog.V(4).Infof("Convert %s capacity value from %f to %f for %s - %s", rType, oldValue, capacityValue, entityType, entityID)

		}
		commBoughtBuilder.Capacity(capacityValue)

		// set additional attribute
		if commodityAttrSetter != nil && commodityAttrSetter.Settable(rType) {
			commodityAttrSetter.Set(rType, commBoughtBuilder)
		}
		commSold, err := commBoughtBuilder.Create()
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to build commodity sold: %s", err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder generalBuilder) getResourceCommoditiesBought(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType, converter *converter, commodityAttrSetter *attributeSetter) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for _, rType := range resourceTypesList {
		cType, exist := rTypeMapping[rType]
		if !exist {
			glog.Errorf("Commodity type %s bought by %s is not supported", rType, entityType)
			continue
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)

		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := builder.metricsSink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s: %s", rType, entityType, entityID, err)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)
		if converter != nil && converter.Convertible(rType) {
			usedValue = converter.Convert(rType, usedValue)
		}
		commSoldBuilder.Used(usedValue)

		reservedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Reservation)
		reservedMetric, err := builder.metricsSink.GetMetric(reservedMetricUID)
		if err != nil {
			// TODO return? Or use Error?
			// not every commodity has reserved value.
			glog.V(4).Infof("Don't find %s reservation for %s %s: %s", rType, entityType, entityID, err)
		} else {
			reservedValue := reservedMetric.GetValue().(float64)
			if reservedValue != 0 {
				if converter != nil && converter.Convertible(rType) {
					reservedValue = converter.Convert(rType, reservedValue)
				}
				commSoldBuilder.Reservation(reservedValue)
			}
		}

		// set additional attribute
		if commodityAttrSetter != nil && commodityAttrSetter.Settable(rType) {
			commodityAttrSetter.Set(rType, commSoldBuilder)
		}

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to build commodity bought: %s", err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}
