package dtofactory

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"strings"

	"github.com/golang/glog"
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	cpuCommodities = []metrics.ResourceType{
		metrics.CPU,
		metrics.CPURequest,
		metrics.CPULimitQuota,
		metrics.CPURequestQuota,
	}

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

	cpuRequestCommodity = []metrics.ResourceType{
		metrics.CPURequest,
	}

	memRequestCommodity = []metrics.ResourceType{
		metrics.MemoryRequest,
	}

	commodityBought = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}

	cpuRequestQuotaCommodityBought = []metrics.ResourceType{
		metrics.CPURequestQuota,
	}

	memoryRequestQuotaCommodityBought = []metrics.ResourceType{
		metrics.MemoryRequestQuota,
	}

	cpuLimitQuotaCommodityBought = []metrics.ResourceType{
		metrics.CPULimitQuota,
	}

	memoryLimitQuotaCommodityBought = []metrics.ResourceType{
		metrics.MemoryLimitQuota,
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
			isCpuRequestSet := !container.Resources.Requests.Cpu().IsZero()
			if !isCpuRequestSet {
				glog.V(4).Infof("Container[%s] has no request set for CPU", name)
			}
			isMemRequestSet := !container.Resources.Requests.Memory().IsZero()
			if !isMemRequestSet {
				glog.V(4).Infof("Container[%s] has no request set for Memory", name)
			}
			commoditiesSold, err := builder.getCommoditiesSold(name, containerId, containerMId, nodeCPUFrequency,
				isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet)
			if err != nil {
				glog.Errorf("failed to create commoditiesSold for container[%s]: %v", name, err)
				continue
			}
			ebuilder.SellsCommodities(commoditiesSold)

			//2. commodities bought
			commoditiesBought, err := builder.getCommoditiesBought(podId, name, containerMId, nodeCPUFrequency,
				isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet)
			if err != nil {
				glog.Errorf("failed to create commoditiesBought for container[%s]: %v", name, err)
				continue
			}
			provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER_POD, podId)
			ebuilder.Provider(provider).BuysCommodities(commoditiesBought)

			//3. set properties
			properties := builder.getContainerProperties(pod, i)
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

//vCPU, vMem, vCPURequest, vMemRequest and Application are sold by Container
func (builder *containerDTOBuilder) getCommoditiesSold(containerName, containerId, containerMId string, cpuFrequency float64,
	isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet bool) ([]*proto.CommodityDTO, error) {

	var result []*proto.CommodityDTO
	containerEntityType := metrics.ContainerType

	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, cpuCommodities...)
	//1a. vCPU
	cpuAttrSetter := NewCommodityAttrSetter()
	cpuAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isCpuLimitSet) }, metrics.CPU)

	cpuCommodities, err := builder.getResourceCommoditiesSold(containerEntityType, containerMId, cpuCommoditySold, converter, cpuAttrSetter)
	if err != nil {
		return nil, err
	}

	//1b. vMem
	memAttrSetter := NewCommodityAttrSetter()
	memAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isMemLimitSet) }, metrics.Memory)

	memCommodities, err := builder.getResourceCommoditiesSold(containerEntityType, containerMId, memCommoditySold, nil, memAttrSetter)
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

	//1c. vCPURequest
	// Container sells vCPURequest commodity only if CPU request is set on the container
	if isCpuRequestSet {
		cpuRequestCommodities, err := builder.createCommoditiesSold(containerEntityType, cpuRequestCommodity, containerMId, converter)
		if err != nil {
			return nil, err
		}
		result = append(result, cpuRequestCommodities...)
	}

	//1d. vMemRequest
	// Container sells vMemRequest commodity only if memory request is set on the container
	if isMemRequestSet {
		memRequestCommodities, err := builder.createCommoditiesSold(containerEntityType, memRequestCommodity, containerMId, nil)
		if err != nil {
			return nil, err
		}
		result = append(result, memRequestCommodities...)
	}

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

// createCommoditiesSold creates a slice of resource commodities sold of the given resourceType.
func (builder *containerDTOBuilder) createCommoditiesSold(entityType metrics.DiscoveredEntityType,
	resourceTypes []metrics.ResourceType, containerMId string, converter *converter) ([]*proto.CommodityDTO, error) {
	attrSetter := NewCommodityAttrSetter()
	attrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, resourceTypes...)
	commoditiesSold, err := builder.getResourceCommoditiesSold(entityType, containerMId, resourceTypes, converter, attrSetter)
	return commoditiesSold, err
}

// vCPU, vMem, vCPURequest, vMemRequest, vCPULimitQuota, vCPURequestQuota, vMemLimitQuota, vMemRequestQuota and VMPMAccess
// are bought by Container from Pod;
// the VMPMAccess is to bind the container to the hosting pod.
func (builder *containerDTOBuilder) getCommoditiesBought(podId, containerName, containerMId string, cpuFrequency float64,
	isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet bool) ([]*proto.CommodityDTO, error) {
	var result []*proto.CommodityDTO
	containerEntityType := metrics.ContainerType

	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, cpuCommodities...)
	//1a. vCPU & vMem
	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	commodities, err := builder.getResourceCommoditiesBought(containerEntityType, containerMId, commodityBought, converter, attributeSetter)
	if err != nil {
		return nil, err
	}
	if len(commodities) != len(commodityBought) {
		err = fmt.Errorf("mismatch num of commidities (%d Vs. %d) for container:%s, %s", len(commodities), len(commodityBought), containerName, containerMId)
		glog.Error(err)
		return nil, err
	}
	result = append(result, commodities...)

	//1b. vCPURequest, vCPURequestQuota
	// Container buys vCPURequest and vCPURequestQuota commodities only if CPU request is set on the container
	if isCpuRequestSet {
		cpuRequestCommBought, err := builder.createRequestCommodityBought(containerEntityType, metrics.CPURequest, containerMId, converter)
		if err != nil {
			return nil, err
		}
		result = append(result, cpuRequestCommBought)
		cpuRequestQuotaCommBought, err := builder.createCommoditiesBought(containerEntityType, cpuRequestQuotaCommodityBought, containerMId, converter)
		if err != nil {
			return nil, err
		}
		result = append(result, cpuRequestQuotaCommBought...)
	}

	//1c. vMemoryRequest, vMemoryRequestQuota
	// Container buys vMemoryRequest and vMemoryRequestQuota commodities only if memory request is set on the container
	if isMemRequestSet {
		memRequestCommSold, err := builder.createRequestCommodityBought(containerEntityType, metrics.MemoryRequest, containerMId, nil)
		if err != nil {
			return nil, err
		}
		result = append(result, memRequestCommSold)
		memRequestQuotaCommBought, err := builder.createCommoditiesBought(containerEntityType, memoryRequestQuotaCommodityBought, containerMId, nil)
		if err != nil {
			return nil, err
		}
		result = append(result, memRequestQuotaCommBought...)
	}

	//1d. vCPULimitQuota
	// Container buys vCPULimitQuota commodity only if CPU limit is set on the container
	if isCpuLimitSet {
		cpuLimitQuotaCommBought, err := builder.createCommoditiesBought(containerEntityType, cpuLimitQuotaCommodityBought, containerMId, converter)
		if err != nil {
			return nil, err
		}
		result = append(result, cpuLimitQuotaCommBought...)
	}

	//1e. vMemoryLimitQuota
	// Container buys vMemoryLimitQuota commodity only if memory limit is set on the container
	if isMemLimitSet {
		memLimitQuotaCommBought, err := builder.createCommoditiesBought(containerEntityType, memoryLimitQuotaCommodityBought, containerMId, nil)
		if err != nil {
			return nil, err
		}
		result = append(result, memLimitQuotaCommBought...)
	}

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

// createRequestCommodityBought creates a request commodity bought by the container of the given resource type. The used
// value of request commodity bought is the configured resource requests capacity on the container.
func (builder *containerDTOBuilder) createRequestCommodityBought(entityType metrics.DiscoveredEntityType,
	resourceType metrics.ResourceType, containerMId string, converter *converter) (*proto.CommodityDTO, error) {
	cType, exist := rTypeMapping[resourceType]
	if !exist {
		glog.Errorf("%s::%s cannot build bought commodity %s : Unsupported commodity type",
			entityType, containerMId, resourceType)

	}
	commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
	// Used value of request commodity bought by the container is the configured resource requests capacity
	usedValue, err := builder.metricValue(entityType, containerMId, resourceType, metrics.Capacity, converter)
	if err != nil {
		glog.Errorf("%s::%s cannot build bought commodity %s : %v", entityType, containerMId, resourceType, err)
		return nil, err
	}
	commBoughtBuilder.Used(usedValue)
	commBoughtBuilder.Peak(usedValue)
	commBoughtBuilder.Resizable(true)
	commBought, err := commBoughtBuilder.Create()
	if err != nil {
		glog.Errorf("%s::%s cannot build bought commodity %s : %v", entityType, containerMId, resourceType, err)
		return nil, err
	}
	return commBought, nil
}

// createCommoditiesBought creates a slice of resource commodities bought of the given resourceType.
func (builder *containerDTOBuilder) createCommoditiesBought(entityType metrics.DiscoveredEntityType,
	resourceTypes []metrics.ResourceType, containerMId string, converter *converter) ([]*proto.CommodityDTO, error) {
	attrSetter := NewCommodityAttrSetter()
	attrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, resourceTypes...)
	commoditiesBought, err := builder.getResourceCommoditiesBought(entityType, containerMId, resourceTypes, converter, attrSetter)
	return commoditiesBought, err
}

func (builder *containerDTOBuilder) getContainerProperties(pod *api.Pod, index int) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	podProperties := builder.addPodProperties(pod, index)
	properties = append(properties, podProperties...)

	ns := stitching.DefaultPropertyNamespace
	podidattr := stitching.PodID
	idattr := stitching.ContainerID
	fullidattr := stitching.ContainerFullID
	containerID := getContainerStitchingProperty(pod, index)
	podID := string(pod.UID)
	// Short containerID
	if len(containerID) >= stitching.ContainerIDlen {
		cntid := containerID[:stitching.ContainerIDlen]
		idStitchingProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &ns,
			Name:      &idattr,
			Value:     &cntid,
		}
		properties = append(properties, idStitchingProperty)
	}
	// Full containerID
	if containerID != "" {
		fullIdStitchingProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &ns,
			Name:      &fullidattr,
			Value:     &containerID,
		}
		properties = append(properties, fullIdStitchingProperty)
	}
	// Full podID
	if pod.UID != "" && index < 1 {
		podStitchingProperty := &proto.EntityDTO_EntityProperty{
			Namespace: &ns,
			Name:      &podidattr,
			Value:     &podID,
		}
		properties = append(properties, podStitchingProperty)
	}
	return properties
}

// Get the properties of the hosting pod.
func (builder *containerDTOBuilder) addPodProperties(pod *api.Pod, index int) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	podProperties := property.AddHostingPodProperties(pod.Namespace, pod.Name, index)
	properties = append(properties, podProperties...)

	return properties
}

// Get the stitching property for Application.
func getContainerStitchingProperty(pod *api.Pod, index int) string {
	// Parse the container id from the status, it is either in the form of
	// containerID: docker://986c8fc7247d1ff047f06e8e3487029b89baef1e908d3e334dd94aaa3bf4bc39
	// or
	// containerID: cri-o://81ab6978aa3252bc53cfaf7d23b8b1bf5f8f27ff4b9e79a0fbaffebeddaeeb4b
	if len(pod.Status.ContainerStatuses) <= index {
		return ""
	}
	containerStatus := &(pod.Status.ContainerStatuses[index])
	cntiduri := strings.Split(containerStatus.ContainerID, "://")
	if len(cntiduri) <= 1 {
		return ""
	}
	return cntiduri[1]
}
