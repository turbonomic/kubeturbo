package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	commoditySold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}

	cpuCommoditySold = []metrics.ResourceType{
		metrics.CPU,
	}

	memCommoditySold = []metrics.ResourceType{
		metrics.Memory,
	}

	commodityBought = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}
)

type containerDTOBuilder struct {
	generalBuilder
}

func NewContainerDTOBuilder(sink *metrics.EntityMetricSink) *containerDTOBuilder {
	return &containerDTOBuilder{
		generalBuilder: newGeneralBuilder(sink),
	}
}

// get cpu frequency
func (builder *containerDTOBuilder) getNodeCPUFrequency(pod *api.Pod) (float64, error) {
	key := util.NodeKeyFromPodFunc(pod)
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, key, metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		err := fmt.Errorf("Failed to get cpu frequency from sink for node %s: %v", key, err)
		glog.Error(err)
		return 0.0, err
	}

	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	return cpuFrequency, nil
}

func (builder *containerDTOBuilder) BuildDTOs(pods []*api.Pod) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	for _, pod := range pods {
		nodeCPUFrequency, err := builder.getNodeCPUFrequency(pod)
		if err != nil {
			glog.Errorf("failed to build ContainerDTOs for pod[%s]: %v", pod.Name, err)
			continue
		}
		podId := string(pod.UID)
		podMId := util.PodMetricIdAPI(pod)

		for i := range pod.Spec.Containers {
			container := &(pod.Spec.Containers[i])

			containerId := util.ContainerIdFunc(podId, i)
			containerMId := util.ContainerMetricId(podMId, container.Name)

			name := util.ContainerNameFunc(pod, container)
			ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER, containerId).DisplayName(name)

			//1. commodities sold
			isCpuLimitSet := !container.Resources.Limits.Cpu().IsZero()
			if !isCpuLimitSet {
				glog.V(4).Infof("Container[%s] has no limit set for CPU", name)
			}
			isMemLimitSet := !container.Resources.Limits.Memory().IsZero()
			if !isMemLimitSet {
				glog.V(4).Infof("Container[%s] has no limit set for Memory", name)
			}
			commoditiesSold, err := builder.getCommoditiesSold(name, containerId, containerMId, nodeCPUFrequency, isCpuLimitSet, isMemLimitSet)
			if err != nil {
				glog.Errorf("failed to create commoditiesSold for container[%s]: %v", name, err)
				continue
			}
			ebuilder.SellsCommodities(commoditiesSold)

			//2. commodities bought
			commoditiesBought, err := builder.getCommoditiesBought(podId, name, containerMId, nodeCPUFrequency)
			if err != nil {
				glog.Errorf("failed to create commoditiesBought for container[%s]: %v", name, err)
				continue
			}
			provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER_POD, podId)
			ebuilder.Provider(provider).BuysCommodities(commoditiesBought)

			//3. set properties
			properties := builder.addPodProperties(pod, i)
			ebuilder.WithProperties(properties)

			//ebuilder.Monitored(util.Monitored(pod))
			ebuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

			truep := true
			controllable := util.Controllable(pod)
			ebuilder.ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
				ProviderMustClone: &truep,
				Controllable:      &controllable,
			})

			//4. build entityDTO
			dto, err := ebuilder.Create()
			if err != nil {
				glog.Errorf("failed to build Container[%s] entityDTO: %v", name, err)
			}

			result = append(result, dto)
		}
	}

	return result, nil
}

//vCPU, vMem, Application are sold by Container to Application
func (builder *containerDTOBuilder) getCommoditiesSold(containerName, containerId, containerMId string,
	cpuFrequency float64, isCpuLimitSet, isMemLimitSet bool) ([]*proto.CommodityDTO, error) {

	var result []*proto.CommodityDTO

	//1a. vCPU
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU)

	cpuAttrSetter := NewCommodityAttrSetter()
	cpuAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isCpuLimitSet) }, metrics.CPU)

	cpuCommodities, err := builder.getResourceCommoditiesSold(metrics.ContainerType, containerMId, cpuCommoditySold, converter, cpuAttrSetter)
	if err != nil {
		return nil, err
	}

	//1b. vMem
	memAttrSetter := NewCommodityAttrSetter()
	memAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isMemLimitSet) }, metrics.Memory)

	memCommodities, err := builder.getResourceCommoditiesSold(metrics.ContainerType, containerMId, memCommoditySold, nil, memAttrSetter)
	if err != nil {
		return nil, err
	}

	commodities := append(cpuCommodities, memCommodities...)
	if len(commodities) != len(commoditySold) {
		err = fmt.Errorf("mismatch num of commidities (%d Vs. %d) for container:%s, %s", len(commodities), len(commoditySold), containerName, containerMId)
		glog.Error(err)
		return nil, err
	}
	result = append(result, commodities...)

	//2. Application
	appCommodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(containerId).
		Capacity(applicationCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}
	result = append(result, appCommodity)

	return result, nil
}

// vCPU, vMem and VMPMAccess are bought by Container from Pod;
// the VMPMAccess is to bind the container to the hosting pod.
func (builder *containerDTOBuilder) getCommoditiesBought(podId, containerName, containerMId string, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var result []*proto.CommodityDTO

	//1. vCPU & vMem
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU)

	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	commodities, err := builder.getResourceCommoditiesBought(metrics.ContainerType, containerMId, commodityBought, converter, attributeSetter)
	if err != nil {
		return nil, err
	}
	if len(commodities) != len(commodityBought) {
		err = fmt.Errorf("mismatch num of commidities (%d Vs. %d) for container:%s, %s", len(commodities), len(commodityBought), containerName, containerMId)
		glog.Error(err)
		return nil, err
	}
	result = append(result, commodities...)

	//2. VMPMAccess
	podAccessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(podId).
		Create()
	if err != nil {
		return nil, err
	}
	result = append(result, podAccessComm)

	return result, nil
}

// Get the properties of the hosting pod.
func (builder *containerDTOBuilder) addPodProperties(pod *api.Pod, index int) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	podProperties := property.AddHostingPodProperties(pod.Namespace, pod.Name, index)
	properties = append(properties, podProperties...)

	return properties
}
