package dtofactory

import (
	"math"

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
		metrics.CPULimitQuota:      proto.CommodityDTO_VCPU_LIMIT_QUOTA,
		metrics.MemoryLimitQuota:   proto.CommodityDTO_VMEM_LIMIT_QUOTA,
		metrics.CPURequestQuota:    proto.CommodityDTO_VCPU_REQUEST_QUOTA,
		metrics.MemoryRequestQuota: proto.CommodityDTO_VMEM_REQUEST_QUOTA,
		metrics.NumPods:            proto.CommodityDTO_NUMBER_CONSUMERS,
		metrics.VStorage:           proto.CommodityDTO_VSTORAGE,
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

	metricValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Used, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}

	// Set used value as the average of multiple used metric points
	commSoldBuilder.Used(metricValue.Avg)
	// Set peak value as the peak of multiple used metric points
	commSoldBuilder.Peak(metricValue.Peak)

	// set capacity value
	capacityMetricValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Capacity, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}
	// Capacity metric is always a single data point. Use Avg to refer to the single point value
	commSoldBuilder.Capacity(capacityMetricValue.Avg)

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
	converter *converter) (metrics.MetricValue, error) {
	metricValue := metrics.MetricValue{}

	metricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, resourceType, metricProp)
	metric, err := builder.metricsSink.GetMetric(metricUID)
	if err != nil {
		return metricValue, fmt.Errorf("missing metrics %s", metricProp)
	}

	value := metric.GetValue()
	switch value.(type) {
	case []metrics.Point:
		var sum float64
		var peak float64
		for _, point := range value.([]metrics.Point) {
			sum += point.Value
			peak = math.Max(peak, point.Value)
		}
		metricValue.Avg = sum / float64(len(value.([]metrics.Point)))
		metricValue.Peak = peak
	case float64:
		metricValue.Avg = value.(float64)
		metricValue.Peak = value.(float64)
	default:
		return metricValue, fmt.Errorf("unsupported metric value type %t", metric.GetValue())
	}
	if converter != nil && converter.Convertible(resourceType) {
		oldAvgValue := metricValue.Avg
		oldPeakValue := metricValue.Peak
		metricValue.Avg = converter.Convert(resourceType, metricValue.Avg)
		metricValue.Peak = converter.Convert(resourceType, metricValue.Peak)
		glog.V(4).Infof("%s:%s converted %s:%s average value from %f to %f", entityType, entityID,
			resourceType, metricProp, oldAvgValue, metricValue.Avg)
		glog.V(4).Infof("%s:%s converted %s:%s peak value from %f to %f", entityType, entityID,
			resourceType, metricProp, oldPeakValue, metricValue.Peak)
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
		metricValue, err := builder.metricValue(entityType, entityID, rType, metrics.Used, converter)
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s cannot build bought commodity %s : missing metrics %s",
				entityType, entityID, rType, metrics.Used)
			continue
		}
		commBoughtBuilder.Used(metricValue.Avg)

		// set peak value as the used value
		commBoughtBuilder.Peak(metricValue.Peak)

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

	metricValue, err := builder.metricValue(entityType, entityID,
		resourceType, metrics.Used, converter)
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}

	// Set used value as the average of multiple usage metric points
	commBoughtBuilder.Used(metricValue.Avg)
	// Set peak value as the peak of multiple usage metric points
	commBoughtBuilder.Peak(metricValue.Peak)

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
