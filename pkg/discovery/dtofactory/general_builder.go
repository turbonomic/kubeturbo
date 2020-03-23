package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"fmt"

	"github.com/golang/glog"
)

var (
	// This map maps resource type to commodity types defined in ProtoBuf.
	rTypeMapping = map[metrics.ResourceType]proto.CommodityDTO_CommodityType{
		metrics.CPU:                proto.CommodityDTO_VCPU,
		metrics.Memory:             proto.CommodityDTO_VMEM,
		metrics.CPURequest:         proto.CommodityDTO_VCPU_REQUEST,
		metrics.MemoryRequest:      proto.CommodityDTO_VMEM_REQUEST,
		metrics.CPUProvisioned:     proto.CommodityDTO_CPU_PROVISIONED,
		metrics.MemoryProvisioned:  proto.CommodityDTO_MEM_PROVISIONED,
		metrics.Transaction:        proto.CommodityDTO_TRANSACTION,
		metrics.CPUQuota:           proto.CommodityDTO_CPU_ALLOCATION,
		metrics.MemoryQuota:        proto.CommodityDTO_MEM_ALLOCATION,
		metrics.CPURequestQuota:    proto.CommodityDTO_CPU_REQUEST_ALLOCATION,
		metrics.MemoryRequestQuota: proto.CommodityDTO_MEM_REQUEST_ALLOCATION,
		metrics.NumPods:            proto.CommodityDTO_NUMBER_CONSUMERS,
		metrics.VStorage:           proto.CommodityDTO_VSTORAGE,
		metrics.StorageAmount:      proto.CommodityDTO_STORAGE_AMOUNT,
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

// Create commodity DTOs for the given list of resources
// Note: cpuFrequency is the speed of CPU for a node. It is passed in as a parameter to convert
// the cpu resource metric values from Kubernetes that is specified in number of cores to MHz.
func (builder generalBuilder) getResourceCommoditiesSold(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType,
	converter *converter, commodityAttrSetter *attributeSetter) ([]*proto.CommodityDTO, error) {

	var resourceCommoditiesSold []*proto.CommodityDTO
	for _, rType := range resourceTypesList {

		commSold, err := builder.getSoldResourceCommodityWithKey(entityType, entityID,
			rType, "", converter, commodityAttrSetter)
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s: cannot build sold commodity %s : %s",
				entityType, entityID, rType, err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder generalBuilder) getSoldResourceCommodityWithKey(entityType metrics.DiscoveredEntityType, entityID string,
	resourceType metrics.ResourceType, commKey string,
	converter *converter, commodityAttrSetter *attributeSetter) (*proto.CommodityDTO, error) {

	var resourceCommoditySold *proto.CommodityDTO
	cType, exist := rTypeMapping[resourceType]
	if !exist {
		return nil, fmt.Errorf("unsupported commodity type %s", resourceType)
	}

	// check for the unit converter for cpu resources
	if metrics.IsCPUType(resourceType) && converter == nil {
		return nil, fmt.Errorf("missing cpu converter")
	}

	commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)

	// set used value
	usedValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Used, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}

	commSoldBuilder.Used(usedValue)

	// set peak value as the used value
	commSoldBuilder.Peak(usedValue)

	// set capacity value
	capacityValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Capacity, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}
	commSoldBuilder.Capacity(capacityValue)

	// set additional attribute
	if commodityAttrSetter != nil && commodityAttrSetter.Settable(resourceType) {
		commodityAttrSetter.Set(resourceType, commSoldBuilder)
	}
	// set commodity key
	if commKey != "" {
		commSoldBuilder.Key(commKey)
	}
	resourceCommoditySold, err = commSoldBuilder.Create()
	if err != nil {
		return nil, err
	}
	return resourceCommoditySold, nil
}

func (builder generalBuilder) metricValue(entityType metrics.DiscoveredEntityType, entityID string,
	resourceType metrics.ResourceType, metricProp metrics.MetricProp,
	converter *converter) (float64, error) {

	metricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, resourceType, metricProp)
	metric, err := builder.metricsSink.GetMetric(metricUID)
	if err != nil {
		return 0, fmt.Errorf("missing metrics %s", metricProp)
	}
	metricValue := metric.GetValue().(float64)
	if converter != nil && converter.Convertible(resourceType) {
		oldValue := metricValue
		metricValue = converter.Convert(resourceType, metricValue)
		glog.V(4).Infof("%s:%s converted %s:%s value from %f to %f", entityType, entityID,
			resourceType, metricProp, oldValue, metricValue)
	}

	return metricValue, nil
}

func (builder generalBuilder) getResourceCommoditiesBought(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType,
	converter *converter, commodityAttrSetter *attributeSetter) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesBought []*proto.CommodityDTO
	for _, rType := range resourceTypesList {
		cType, exist := rTypeMapping[rType]
		if !exist {
			glog.Errorf("%s::%s cannot build bought commodity %s : Unsupported commodity type",
				entityType, entityID, rType)
			continue
		}
		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)

		// set used value
		usedValue, err := builder.metricValue(entityType, entityID, rType, metrics.Used, converter)
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s cannot build bought commodity %s : missing metrics %s",
				entityType, entityID, rType, metrics.Used)
			continue
		}
		commBoughtBuilder.Used(usedValue)

		// set peak value as the used value
		commBoughtBuilder.Peak(usedValue)

		// set reservation value if any
		reservedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID,
			rType, metrics.Reservation)
		reservedMetric, err := builder.metricsSink.GetMetric(reservedMetricUID)
		if err != nil {
			// not every commodity has reserved value.
			glog.V(4).Infof("%s::%s Reservation not set for commodity %s: %s",
				entityType, entityID, rType, err)
		} else {
			reservedValue := reservedMetric.GetValue().(float64)
			if reservedValue != 0 {
				if converter != nil && converter.Convertible(rType) {
					reservedValue = converter.Convert(rType, reservedValue)
				}
				commBoughtBuilder.Reservation(reservedValue)
			}
		}

		// set additional attribute
		if commodityAttrSetter != nil && commodityAttrSetter.Settable(rType) {
			commodityAttrSetter.Set(rType, commBoughtBuilder)
		}

		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s: cannot build bought commodity %s : %s",
				entityType, entityID, rType, err)
			continue
		}
		resourceCommoditiesBought = append(resourceCommoditiesBought, commBought)
	}
	return resourceCommoditiesBought, nil
}

func (builder generalBuilder) getResourceCommodityBoughtWithKey(entityType metrics.DiscoveredEntityType, entityID string,
	resourceType metrics.ResourceType, commKey string,
	converter *converter, commodityAttrSetter *attributeSetter) (*proto.CommodityDTO, error) {
	cType, exist := rTypeMapping[resourceType]
	if !exist {
		return nil, fmt.Errorf("unsupported commodity type %s", resourceType)
	}

	// check for the unit converter for cpu resources
	if metrics.IsCPUType(resourceType) && converter == nil {
		return nil, fmt.Errorf("missing cpu converter")
	}

	commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
	// set used value
	usedValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Used, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}

	commBoughtBuilder.Used(usedValue)

	// set peak value as the used value
	commBoughtBuilder.Peak(usedValue)

	// set reservation value if any
	reservationValue, _ := builder.metricValue(entityType, entityID,
		resourceType, metrics.Reservation, converter)
	if reservationValue != 0 {
		glog.V(4).Infof("%s::%s Reservation set for commodity %s: %s",
			entityType, entityID, resourceType, err)
		commBoughtBuilder.Reservation(reservationValue)
	}
	// set additional attribute
	if commodityAttrSetter != nil && commodityAttrSetter.Settable(resourceType) {
		commodityAttrSetter.Set(resourceType, commBoughtBuilder)
	}
	// set commodity key
	if commKey != "" {
		commBoughtBuilder.Key(commKey)
	}
	commBought, err := commBoughtBuilder.Create()
	if err != nil {
		return nil, err
	}

	return commBought, nil
}

// get cpu frequency
func (builder generalBuilder) getNodeCPUFrequency(nodeKey string) (float64, error) {
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, nodeKey, metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		err := fmt.Errorf("failed to get cpu frequency from sink for node %s: %v", nodeKey, err)
		return 0.0, err
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	glog.V(4).Infof("CPU frequency for node %s: %f", nodeKey, cpuFrequency)
	return cpuFrequency, nil
}
