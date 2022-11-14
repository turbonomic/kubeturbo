package dtofactory

import (
	"fmt"
	"strings"

	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/features"

	"github.com/golang/glog"
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
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

	throttlingCommodity = []metrics.ResourceType{
		metrics.VCPUThrottling,
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

func NewContainerDTOBuilder(sink *metrics.EntityMetricSink, config *CommodityConfig) *containerDTOBuilder {
	return &containerDTOBuilder{
		generalBuilder: newGeneralBuilder(sink, config),
	}
}

func (builder *containerDTOBuilder) BuildEntityDTOs(pods []*api.Pod) ([]*proto.EntityDTO, []string) {
	var result []*proto.EntityDTO
	var err error
	var sidecars []string

	for _, pod := range pods {
		podId := string(pod.UID)
		podMId := util.PodMetricIdAPI(pod)
		controllerUID := ""
		if util.HasController(pod) {
			// Get controllerUID only if Pod is deployed by a K8s controller.
			controllerUID, err = util.GetControllerUID(pod, builder.metricsSink)
			if err != nil {
				glog.V(3).Infof("Cannot find controller UID for pod %s/%s, %v", pod.Namespace, pod.Name, err)
				continue
			}
		}
		for i := range pod.Spec.Containers {
			container := &(pod.Spec.Containers[i])

			containerId := util.ContainerIdFunc(podId, i)
			containerMId := util.ContainerMetricId(podMId, container.Name)

			name := util.ContainerNameFunc(pod, container)
			ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER, containerId).DisplayName(name)

			if controllerUID != "" {
				// To connect Container to ContainerSpec entity, Container is Controlled by the associated ContainerSpec.
				containerSpecId := util.ContainerSpecIdFunc(controllerUID, container.Name)
				ebuilder.ControlledBy(containerSpecId)
				if builder.isInjectedSidecarContainer(containerMId) {
					// Add the containerSpec id to the set
					sidecars = append(sidecars, containerSpecId)
				}
			}

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
			commoditiesSold, err := builder.getCommoditiesSold(name, containerId, containerMId,
				isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet)
			if err != nil {
				glog.Warningf("Failed to create commoditiesSold for container[%s]: %v", name, err)
				continue
			}
			ebuilder.SellsCommodities(commoditiesSold)

			//2. commodities bought
			commoditiesBought, err := builder.getCommoditiesBought(podId, name, containerMId,
				isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet)
			if err != nil {
				glog.Warningf("failed to create commoditiesBought for container[%s]: %v", name, err)
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
			monitored := true
			powerState := proto.EntityDTO_POWERED_ON
			if !util.PodIsReady(pod) {
				controllable = false
				monitored = false
				powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
			}

			//4. build entityDTO
			dto, err := ebuilder.
				ContainerData(builder.createContainerData(isCpuLimitSet, isMemLimitSet)).
				ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
					ProviderMustClone: &truep,
					Controllable:      &controllable,
				}).
				Monitored(monitored).
				WithPowerState(powerState).
				Create()
			if err != nil {
				glog.Errorf("failed to build Container[%s] entityDTO: %v", name, err)
			}

			result = append(result, dto)
		}
	}

	return result, sidecars
}

// isInjectedSidecarContainer checks the metric "IsInjectedSidecar" which tells if this container exists in the
// parents pod.template.spec or not.
// If it does not exist in the parent its an injected sidecar.
func (builder *containerDTOBuilder) isInjectedSidecarContainer(containerMId string) bool {
	metricUID := metrics.GenerateEntityStateMetricUID(metrics.ContainerType, containerMId, metrics.IsInjectedSidecar)
	metric, err := builder.metricsSink.GetMetric(metricUID)
	if err != nil {
		glog.Warningf("Failed to get IsInjectedSidecar value for container %s: %v", containerMId, err)
		// we consider it a normal container on failures
		return false
	}
	return metric.GetValue().(bool)
}

// vCPU, vMem, vCPURequest, vMemRequest and Application are sold by Container.
func (builder *containerDTOBuilder) getCommoditiesSold(containerName, containerId, containerMId string,
	isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet bool) ([]*proto.CommodityDTO, error) {

	var result []*proto.CommodityDTO
	containerEntityType := metrics.ContainerType

	//1a. vCPU
	cpuAttrSetter := NewCommodityAttrSetter()
	cpuAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isCpuLimitSet) }, metrics.CPU)
	cpuCommodities := builder.getResourceCommoditiesSold(containerEntityType, containerMId, cpuCommoditySold, nil, cpuAttrSetter)
	result = append(result, cpuCommodities...)

	//1b. vMem
	memAttrSetter := NewCommodityAttrSetter()
	memAttrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isMemLimitSet) }, metrics.Memory)
	memCommodities := builder.getResourceCommoditiesSold(containerEntityType, containerMId, memCommoditySold, nil, memAttrSetter)
	result = append(result, memCommodities...)

	if len(result) != len(commoditySold) {
		return nil, fmt.Errorf("mismatch num of commodities (%d Vs. %d) for container:%s, %s",
			len(result), len(commoditySold), containerName, containerMId)
	}

	//1c. vCPURequest
	// Container sells vCPURequest commodity only if CPU request is set on the container
	if isCpuRequestSet {
		cpuRequestCommodities := builder.createCommoditiesSold(containerEntityType, cpuRequestCommodity, containerMId, nil, true)
		result = append(result, cpuRequestCommodities...)
	}

	//1d. vMemRequest
	// Container sells vMemRequest commodity only if memory request is set on the container
	if isMemRequestSet {
		memRequestCommodities := builder.createCommoditiesSold(containerEntityType, memRequestCommodity, containerMId, nil, true)
		result = append(result, memRequestCommodities...)
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.ThrottlingMetrics) {
		throttlingCommodities := builder.createCommoditiesSold(containerEntityType, throttlingCommodity, containerMId, nil, false)
		result = append(result, throttlingCommodities...)
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
	resourceTypes []metrics.ResourceType, containerMId string, converter *converter, isResizable bool) []*proto.CommodityDTO {
	attrSetter := NewCommodityAttrSetter()
	attrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(isResizable) }, resourceTypes...)
	return builder.getResourceCommoditiesSold(entityType, containerMId, resourceTypes, converter, attrSetter)
}

// vCPU, vMem, vCPURequest, vMemRequest, vCPULimitQuota, vCPURequestQuota, vMemLimitQuota, vMemRequestQuota and VMPMAccess
// are bought by Container from Pod;
// the VMPMAccess is to bind the container to the hosting pod.
func (builder *containerDTOBuilder) getCommoditiesBought(podId, containerName, containerMId string,
	isCpuLimitSet, isMemLimitSet, isCpuRequestSet, isMemRequestSet bool) ([]*proto.CommodityDTO, error) {
	var result []*proto.CommodityDTO
	containerEntityType := metrics.ContainerType

	//1a. vCPU & vMem
	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	commodities := builder.getResourceCommoditiesBought(containerEntityType, containerMId, commodityBought, nil, attributeSetter)
	if len(commodities) != len(commodityBought) {
		return nil, fmt.Errorf("mismatch num of commidities (%d Vs. %d) for container:%s, %s",
			len(commodities), len(commodityBought), containerName, containerMId)
	}
	result = append(result, commodities...)

	//1b. vCPURequest, vCPURequestQuota
	// Container buys vCPURequest and vCPURequestQuota commodities only if CPU request is set on the container
	if isCpuRequestSet {
		cpuRequestCommBought, err := builder.createRequestCommodityBought(containerEntityType, metrics.CPURequest, containerMId, nil)
		if err != nil {
			return nil, err
		}
		result = append(result, cpuRequestCommBought)
		cpuRequestQuotaCommBought := builder.createCommoditiesBought(containerEntityType, cpuRequestQuotaCommodityBought, containerMId, nil)
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
		memRequestQuotaCommBought := builder.createCommoditiesBought(containerEntityType, memoryRequestQuotaCommodityBought, containerMId, nil)
		result = append(result, memRequestQuotaCommBought...)
	}

	//1d. vCPULimitQuota
	// Container buys vCPULimitQuota commodity only if CPU limit is set on the container
	if isCpuLimitSet {
		cpuLimitQuotaCommBought := builder.createCommoditiesBought(containerEntityType, cpuLimitQuotaCommodityBought, containerMId, nil)
		result = append(result, cpuLimitQuotaCommBought...)
	}

	//1e. vMemoryLimitQuota
	// Container buys vMemoryLimitQuota commodity only if memory limit is set on the container
	if isMemLimitSet {
		memLimitQuotaCommBought := builder.createCommoditiesBought(containerEntityType, memoryLimitQuotaCommodityBought, containerMId, nil)
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
		return nil, fmt.Errorf("%s::%s cannot build bought commodity %s : Unsupported commodity type",
			entityType, containerMId, resourceType)
	}
	commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
	// Used value of request commodity bought by the container is the configured resource requests capacity
	metricValue, err := builder.metricValue(entityType, containerMId, resourceType, metrics.Capacity, converter)
	if err != nil {
		return nil, err
	}
	commBoughtBuilder.Used(metricValue.Avg)
	commBoughtBuilder.Peak(metricValue.Peak)
	commBoughtBuilder.Resizable(true)
	return commBoughtBuilder.Create()
}

// createCommoditiesBought creates a slice of resource commodities bought of the given resourceType.
func (builder *containerDTOBuilder) createCommoditiesBought(entityType metrics.DiscoveredEntityType,
	resourceTypes []metrics.ResourceType, containerMId string, converter *converter) []*proto.CommodityDTO {
	attrSetter := NewCommodityAttrSetter()
	attrSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, resourceTypes...)
	return builder.getResourceCommoditiesBought(entityType, containerMId, resourceTypes, converter, attrSetter)
}

// createContainerData creates EntityDTO_ContainerData for the given container entity dto builder.
func (builder *containerDTOBuilder) createContainerData(isCpuLimitSet, isMemLimitSet bool) *proto.EntityDTO_ContainerData {
	return &proto.EntityDTO_ContainerData{
		HasCpuLimit: &isCpuLimitSet,
		HasMemLimit: &isMemLimitSet,
	}
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

// Get the stitching property for Container.
func getContainerStitchingProperty(pod *api.Pod, index int) string {
	// Locate the containerSpec
	containerSpec := &(pod.Spec.Containers[index])
	for _, containerStatus := range pod.Status.ContainerStatuses {
		// Find the containerStatus with the matching container name
		if containerStatus.Name == containerSpec.Name {
			// Parse the container id from the status, it is either in the form of
			// containerID: docker://986c8fc7247d1ff047f06e8e3487029b89baef1e908d3e334dd94aaa3bf4bc39
			// or
			// containerID: cri-o://81ab6978aa3252bc53cfaf7d23b8b1bf5f8f27ff4b9e79a0fbaffebeddaeeb4b
			containerIDURI := strings.Split(containerStatus.ContainerID, "://")
			if len(containerIDURI) <= 1 {
				return ""
			}
			return containerIDURI[1]
		}
	}
	return ""
}
