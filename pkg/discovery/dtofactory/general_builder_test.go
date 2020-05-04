package dtofactory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	pod1  = "pod1"
	node1 = "node1"

	metricsSink = metrics.NewEntityMetricSink()

	cpuUsed_pod1 = metrics.NewEntityResourceMetric(metrics.PodType, pod1, metrics.CPUMili, metrics.Used, 1.0)
	memUsed_pod1 = metrics.NewEntityResourceMetric(metrics.PodType, pod1, metrics.Memory, metrics.Used, 8010812.000000/8)

	cpuCap_pod1 = metrics.NewEntityResourceMetric(metrics.PodType, pod1, metrics.CPUMili, metrics.Capacity, 2.0)
	memCap_pod1 = metrics.NewEntityResourceMetric(metrics.PodType, pod1, metrics.Memory, metrics.Capacity, 8010812.000000/4)

	cpuCap_node1  = metrics.NewEntityResourceMetric(metrics.NodeType, node1, metrics.CPUMili, metrics.Capacity, 4.0)
	cpuUsed_node1 = metrics.NewEntityResourceMetric(metrics.NodeType, node1, metrics.CPUMili, metrics.Used, 2.0)

	memCap_node1  = metrics.NewEntityResourceMetric(metrics.NodeType, node1, metrics.Memory, metrics.Capacity, 8010812.000000)
	memUsed_node1 = metrics.NewEntityResourceMetric(metrics.NodeType, node1, metrics.Memory, metrics.Used, 8010812.000000/4)

	cpuFrequency = 2048.0
	cpuConverter = NewConverter().Set(
		nil,
		metrics.CPUMili)
)

func TestBuildCPUSold(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(cpuUsed_pod1, cpuCap_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	commSold, err := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.CPUMili, "", nil, nil)
	fmt.Printf("%++v\n", commSold)
	fmt.Printf("%++v\n", err)
	assert.Nil(t, err)
	assert.NotNil(t, commSold)
	assert.Equal(t, proto.CommodityDTO_VCPU_MILICORE, commSold.GetCommodityType())
	capacityValue := cpuCap_pod1.GetValue().(float64)
	usedValue := cpuUsed_pod1.GetValue().(float64)
	assert.Equal(t, usedValue, commSold.GetUsed())
	assert.Equal(t, capacityValue, commSold.GetCapacity())
}

func TestBuildMemSold(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(memUsed_pod1, memCap_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	commSold, err := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.Memory, "", nil, nil)
	fmt.Printf("%++v\n", commSold)
	fmt.Printf("%++v\n", err)
	assert.NotNil(t, commSold)
	assert.Equal(t, proto.CommodityDTO_VMEM, commSold.GetCommodityType())
	assert.Nil(t, err)
	assert.Equal(t, memUsed_pod1.GetValue().(float64), commSold.GetUsed())
	assert.Equal(t, memCap_pod1.GetValue().(float64), commSold.GetCapacity())
}

func TestBuildUnsupportedResource(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	commSold, _ := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.MemoryRequest, "", nil, nil)
	assert.Nil(t, commSold)
}

func TestBuildCPUSoldWithMissingCap(t *testing.T) {
	eType := metrics.PodType
	// Missing CPU Capacity
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(cpuUsed_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}
	commSold, err := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.CPUMili, "", nil, nil)
	assert.Nil(t, commSold)
	assert.NotNil(t, err)
}

func TestBuildCPUSoldWithMissingUsed(t *testing.T) {
	eType := metrics.PodType
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(cpuCap_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}
	commSold, err := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.CPUMili, "", nil, nil)
	assert.Nil(t, commSold)
	assert.NotNil(t, err)
}

func TestBuildCommSoldWithKey(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(memUsed_pod1, memCap_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	commSold, err := dtoBuilder.getSoldResourceCommodityWithKey(eType, pod1, metrics.Memory, "key", nil, nil)
	assert.Nil(t, err)
	assert.NotNil(t, commSold)
	assert.Equal(t, proto.CommodityDTO_VMEM, commSold.GetCommodityType())
	assert.Equal(t, "key", commSold.GetKey())
}

func TestBuildCommSold(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(cpuUsed_pod1, cpuCap_pod1, memCap_pod1, memUsed_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	resourceTypesList := []metrics.ResourceType{metrics.CPUMili, metrics.Memory}
	commSoldList, err := dtoBuilder.getResourceCommoditiesSold(eType, pod1, resourceTypesList, nil, nil)
	commMap := make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)

	for _, commSold := range commSoldList {
		commMap[commSold.GetCommodityType()] = commSold
	}

	for _, rType := range resourceTypesList {
		cType, _ := rTypeMapping[rType]
		commSold, _ := commMap[cType]
		assert.NotNil(t, commSold)
		fmt.Printf("%++v\n", commSold)
		fmt.Printf("%++v\n", err)
	}
}

func TestBuildCommBought(t *testing.T) {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(cpuUsed_pod1, memUsed_pod1)

	dtoBuilder := &generalBuilder{
		metricsSink: metricsSink,
	}

	eType := metrics.PodType
	resourceTypesList := []metrics.ResourceType{metrics.CPUMili, metrics.Memory}
	commBoughtList, err := dtoBuilder.getResourceCommoditiesBought(eType, pod1, resourceTypesList, nil, nil)
	commMap := make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)

	for _, commBought := range commBoughtList {
		commMap[commBought.GetCommodityType()] = commBought
	}

	for _, rType := range resourceTypesList {
		cType, _ := rTypeMapping[rType]
		commBought, _ := commMap[cType]
		assert.NotNil(t, commBought)
		fmt.Printf("%++v\n", commBought)
		fmt.Printf("%++v\n", err)
	}
}
