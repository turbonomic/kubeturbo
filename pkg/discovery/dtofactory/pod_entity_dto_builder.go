package dtofactory

import (
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	applicationCommodityDefaultCapacity = 1E10
)

var (
	podResourceCommoditySold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}

	podResourceCommodityBought = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		// TODO, add back provisioned commodity later.
		//metrics.CPUProvisioned,
		//metrics.MemoryProvisioned,
	}
)

type podEntityDTOBuilder struct {
	generalBuilder
	stitchingManager *stitching.StitchingManager
	nodeNameUIDMap   map[string]string
}

func NewPodEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager, nodeNameUIDMap map[string]string) *podEntityDTOBuilder {
	return &podEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink),
		nodeNameUIDMap:   nodeNameUIDMap,
		stitchingManager: stitchingManager,
	}
}

// Build entityDTOs based on the given pod list.
func (builder *podEntityDTOBuilder) BuildEntityDTOs(pods []*api.Pod) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, pod := range pods {

		// id.
		podID := string(pod.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podID)

		// display name.
		displayName := util.GetPodClusterID(pod)
		entityDTOBuilder.DisplayName(displayName)

		// commodities sold.
		commoditiesSold, err := builder.getPodCommoditiesSold(pod)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for pod %s: %s", displayName, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought.
		commoditiesBought, err := builder.getPodCommoditiesBought(pod)
		if err != nil {
			glog.Errorf("Error when create commoditiesBought for pod %s: %s", displayName, err)
			continue
		}
		providerNodeUID, exist := builder.nodeNameUIDMap[pod.Spec.NodeName]
		if !exist {
			glog.Errorf("Error when create commoditiesBought for pod %s: Cannot find uuid for provider "+
				"node.", displayName, err)
			continue
		}
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, providerNodeUID)
		entityDTOBuilder = entityDTOBuilder.Provider(provider)
		entityDTOBuilder.BuysCommodities(commoditiesBought)

		// entities' properties.
		properties, err := builder.getPodProperties(pod)
		if err != nil {
			glog.Errorf("Failed to get required pod properties: %s", err)
			continue
		}
		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		if !util.Monitored(pod) {
			entityDTOBuilder.Monitored(false)
		}

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Pod entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
	}

	return result, nil
}

// Build the sold commodityDTO by each pod. They are:
// vCPU, vMem, ApplicationCommodity.
func (builder *podEntityDTOBuilder) getPodCommoditiesSold(pod *api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// get cpu frequency
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(task.NodeType, util.NodeKeyFromPodFunc(pod), metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		// TODO acceptable return? To get cpu, frequency is required.
		return nil, fmt.Errorf("Failed to get cpu frequency from sink for node %s: %s", key, err)
	}

	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPUProvisioned)

	// attr
	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	// Resource Commodities
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(task.PodType, key, podResourceCommoditySold, converter, attributeSetter)
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// Application commodity
	applicationComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(string(pod.UID)).
		Capacity(applicationCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, applicationComm)

	return commoditiesSold, nil
}

// Build the bought commodityDTO by each pod. They are:
// vCPU, vMem, cpuProvisioned, memProvisioned, access, cluster.
func (builder *podEntityDTOBuilder) getPodCommoditiesBought(pod *api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// get cpu frequency
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(task.NodeType, util.NodeKeyFromPodFunc(pod), metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		glog.Errorf("Failed to get cpu frequency from sink for node %s: %s", util.NodeKeyFromPodFunc(pod), err)
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPUProvisioned)

	// attr
	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	// Resource Commodities.
	resourceCommoditiesBought, err := builder.getResourceCommoditiesBought(task.PodType, key, podResourceCommodityBought, converter, attributeSetter)
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, resourceCommoditiesBought...)

	// Access commodities: selectors.
	for key, value := range pod.Spec.NodeSelector {
		selector := key + "=" + value
		accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
			Key(selector).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, accessComm)
	}

	// Access commodity: schedulable
	if util.Monitored(pod) {
		schedAccessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
			Key(schedAccessCommodityKey).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, schedAccessComm)
	}

	// Cluster commodity.
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(task.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for current Kubernetes Cluster%s", metrics.Cluster)
	} else {
		clusterCommodityKey, ok := clusterInfo.GetValue().(string)
		if !ok {
			glog.Error("Failed to get cluster ID")
		}
		clusterComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
			Key(clusterCommodityKey).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, clusterComm)
	}

	return commoditiesBought, nil
}

// Get the properties of the pod. This includes property related to pod cluster property.
func (builder *podEntityDTOBuilder) getPodProperties(pod *api.Pod) ([]*proto.EntityDTO_EntityProperty, error) {
	var properties []*proto.EntityDTO_EntityProperty
	// additional node cluster info property.
	podProperties := property.BuildPodProperties(pod)
	properties = append(properties, podProperties...)

	podClusterID := util.GetPodClusterID(pod)
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, fmt.Errorf("Cannot find the hosting node ID for pod %s", podClusterID)
	}
	stitchingProperty, err := builder.stitchingManager.BuildStitchingProperty(nodeName, stitching.Stitch)
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for Pod %s: %s", podClusterID, err)
	}
	properties = append(properties, stitchingProperty)

	return properties, nil
}
