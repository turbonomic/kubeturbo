package dtofactory

import (
	"math"
	"sort"

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
		metrics.StorageAmount:      proto.CommodityDTO_STORAGE_AMOUNT,
		metrics.VCPUThrottling:     proto.CommodityDTO_VCPU_THROTTLING,
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
// Note: This function does not return error.
func (builder generalBuilder) getResourceCommoditiesSold(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType,
	converter *converter, commodityAttrSetter *attributeSetter) (resourceCommoditiesSold []*proto.CommodityDTO) {
	for _, rType := range resourceTypesList {
		commSold, err := builder.getSoldResourceCommodityWithKey(entityType, entityID,
			rType, "", converter, commodityAttrSetter)
		if err != nil {
			// skip this commodity
			glog.Warningf("Cannot build sold commodity %s for %s::%s: %v",
				rType, entityType, entityID, err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold
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
		return nil, err
	}

	// Set used value as the average of multiple used metric points
	commSoldBuilder.Used(metricValue.Avg)
	// Set peak value as the peak of multiple used metric points
	commSoldBuilder.Peak(metricValue.Peak)

	// set capacity value
	if resourceType == metrics.VCPUThrottling {
		// This is better then separately posting the capacity into metrics sync
		// and then reading it here.
		commSoldBuilder.Capacity(100)
	} else {
		capacityMetricValue, err := builder.metricValue(entityType, entityID,
			resourceType, metrics.Capacity, converter)
		if err != nil {
			return nil, err
		}
		// Capacity metric is always a single data point. Use Avg to refer to the single point value
		commSoldBuilder.Capacity(capacityMetricValue.Avg)
	}

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

	if resourceType == metrics.Memory && entityType == metrics.NodeType {
		threshold, err := builder.metricValue(entityType, entityID,
			resourceType, metrics.Threshold, nil)
		if err != nil {
			glog.Warningf("Missing threshold value for %v for node %s.", resourceType, entityID)
		}
		// TODO: The settable method for UtilizationThresholdPct can be added to the sdk instead.
		if threshold.Avg > 0 && threshold.Avg <= 100 {
			// Threshold values set in kubelet are in terms of metrics (eg rootfs size) value available.
			thresholdUtilization := 100 - threshold.Avg
			resourceCommoditySold.UtilizationThresholdPct = &thresholdUtilization
		} else {
			glog.Warningf("Threshold value [%.2f] outside range and will not be set for %v for node %s.", threshold.Avg, resourceType, entityID)
		}
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
	switch typedValue := value.(type) {
	case []metrics.Point:
		var sum float64
		var peak float64
		for _, point := range typedValue {
			sum += point.Value
			peak = math.Max(peak, point.Value)
		}
		metricValue.Avg = sum / float64(len(typedValue))
		metricValue.Peak = peak
	case []metrics.ThrottlingCumulative:
		throttled, total, peak, ok := aggregateContainerThrottlingSamples(entityID, typedValue)
		if !ok {
			// We don't have enough samples to calculate this value.
			break
		}

		if total > 0 {
			if throttled > total {
				metricValue.Avg = 100
			} else {
				metricValue.Avg = throttled * 100 / total
			}
		}
		metricValue.Peak = peak
	case float64:
		metricValue.Avg = typedValue
		metricValue.Peak = typedValue
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

// Note: This function does not return error.
func (builder generalBuilder) getResourceCommoditiesBought(entityType metrics.DiscoveredEntityType, entityID string,
	resourceTypesList []metrics.ResourceType,
	converter *converter, commodityAttrSetter *attributeSetter) []*proto.CommodityDTO {
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
			glog.Warningf("Cannot build bought commodity %s for %s::%s: %v",
				rType, entityType, entityID, err)
			continue
		}
		commBoughtBuilder.Used(metricValue.Avg)

		// set peak value as the used value
		commBoughtBuilder.Peak(metricValue.Peak)

		if rType == metrics.VStorage {
			// set commodity key only for vstorahge.
			// currently pods only report and buy rootfs usage.
			commBoughtBuilder.Key("k8s-node-rootfs")
		}

		// set additional attribute
		if commodityAttrSetter != nil && commodityAttrSetter.Settable(rType) {
			commodityAttrSetter.Set(rType, commBoughtBuilder)
		}

		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s: cannot build bought commodity %s: %s",
				entityType, entityID, rType, err)
			continue
		}
		resourceCommoditiesBought = append(resourceCommoditiesBought, commBought)
	}
	return resourceCommoditiesBought
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
		return nil, err
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
	return commBoughtBuilder.Create()
}

// get cpu frequency
func (builder generalBuilder) getNodeCPUFrequency(nodeKey string) (float64, error) {
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, nodeKey, metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		return 0.0, fmt.Errorf("failed to get cpu frequency from sink for node %s: %v", nodeKey, err)
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	glog.V(4).Infof("CPU frequency for node %s: %f", nodeKey, cpuFrequency)
	return cpuFrequency, nil
}

// aggregateContainerThrottlingSamples aggregates the throttling samples collected
// over a period of time but within a single discovery cycle for a single container.
// Throttled value is calculated as the overall percentage from the counter data collected
// from the first and the last sample. The peak is calculated from the individual throttling
// percentages by the diff of counters between two subsequent samples.
func aggregateContainerThrottlingSamples(entityID string, samples []metrics.ThrottlingCumulative) (throttled float64, total float64, peak float64, ok bool) {
	numberOfSamples := len(samples)
	if numberOfSamples <= 1 {
		// We don't have enough samples to calculate this value.
		// Throttling value would appear as zero on the entity.
		if entityID != "" {
			// We log this while calculating the Avg and Peak on an individual container
			// We do not have the container id while aggregating on the container specs
			// but its ok as the information will anyways be a repeated msg only.
			glog.V(3).Infof("Number of samples not enough to calculate throttling value on: %s", entityID)
		}
		return 0, 0, 0, false
	}

	// TODO: The need of this sort could be removed if the metrics sinks
	// are always merged in timed order.
	sort.SliceStable(samples, func(i, j int) bool {
		return samples[i].Timestamp < samples[j].Timestamp
	})

	lastReset := 0
	for i := 0; i < numberOfSamples-1; i++ {
		if samples[i+1].Total < samples[i].Total || samples[i+1].Throttled < samples[i].Throttled {
			// This probably means the counter was reset for some reason
			throttled += (samples[i].Throttled - samples[lastReset].Throttled)
			total += samples[i].Total - samples[lastReset].Total
			lastReset = i + 1
			// we ignore this samples diff for our peak calculations
			continue
		}

		totalSingleSample := samples[i+1].Total - samples[i].Total
		throttledPercent := float64(0)
		if totalSingleSample > 0 {
			throttledSingleSample := samples[i+1].Throttled - samples[i].Throttled
			if throttledSingleSample > totalSingleSample {
				// This is unlikely but possible because of errors at cadvisors end.
				throttledPercent = 100
			} else {
				throttledPercent = throttledSingleSample * 100 / totalSingleSample
			}
		}
		peak = math.Max(peak, throttledPercent)
	}

	// handle last window if there ever was one, else this calculates the diff of the first and the last sample.
	if lastReset != numberOfSamples-1 {
		throttled += (samples[numberOfSamples-1].Throttled - samples[lastReset].Throttled)
		total += samples[numberOfSamples-1].Total - samples[lastReset].Total
	}

	return throttled, total, peak, true
}
