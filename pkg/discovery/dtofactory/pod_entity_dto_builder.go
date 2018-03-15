package dtofactory

import (
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
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

	podResourceCommodityBoughtFromNode = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		// TODO, add back provisioned commodity later.
		//metrics.CPUProvisioned,
		//metrics.MemoryProvisioned,
	}

	podResourceCommodityBoughtFromQuota = []metrics.ResourceType{
		metrics.CPULimit,
		metrics.MemoryLimit,
	}
)

type podEntityDTOBuilder struct {
	generalBuilder
	stitchingManager *stitching.StitchingManager
	nodeNameUIDMap   map[string]string
	quotaNameUIDMap  map[string]string
}

func NewPodEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager,
	nodeNameUIDMap, quotaNameUIDMap map[string]string) *podEntityDTOBuilder {
	return &podEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink),
		nodeNameUIDMap:   nodeNameUIDMap,
		quotaNameUIDMap:  quotaNameUIDMap,
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
		cpuFrequency, err := builder.getNodeCPUFrequency(pod)
		if err != nil {
			glog.Errorf("failed to build pod[%s] EntityDTO: %v", displayName, err)
			continue
		}
		// commodities sold.
		commoditiesSold, err := builder.getPodCommoditiesSold(pod, cpuFrequency)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for pod %s: %s", displayName, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought.
		commoditiesBought, err := builder.getPodCommoditiesBought(pod, cpuFrequency)
		if err != nil {
			glog.Errorf("Error when create commoditiesBought for pod %s: %s", displayName, err)
			continue
		}
		providerNodeUID, exist := builder.nodeNameUIDMap[pod.Spec.NodeName]
		if !exist {
			glog.Errorf("Error when create commoditiesBought for pod %s: Cannot find uuid for provider "+
				"node.", displayName)
			continue
		}
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, providerNodeUID)
		entityDTOBuilder = entityDTOBuilder.Provider(provider)
		entityDTOBuilder.BuysCommodities(commoditiesBought)

		quotaUID, exists := builder.quotaNameUIDMap[pod.Namespace]
		if exists {
			commoditiesBoughtQuota, err := builder.getPodCommoditiesBoughtFromQuota(quotaUID, pod, cpuFrequency)
			if err != nil {
				glog.Errorf("Error when create commoditiesBought for pod %s: %s", displayName, err)
				continue
			}

			provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_DATACENTER, quotaUID)
			entityDTOBuilder = entityDTOBuilder.Provider(provider)
			entityDTOBuilder.BuysCommodities(commoditiesBoughtQuota)
		} else {
			glog.Errorf("Failed to get quota for pod: %s", pod.Namespace)
		}

		// entities' properties.
		properties, err := builder.getPodProperties(pod)
		if err != nil {
			glog.Errorf("Failed to get required pod properties: %s", err)
			continue
		}
		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		if !util.Monitored(pod) {
			entityDTOBuilder.Monitored(false)
			glog.V(3).Infof("Pod %v is not monitored.", displayName)
		}

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Pod entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		glog.V(4).Infof("pod dto: %++v\n", entityDto)
	}

	return result, nil
}

// get cpu frequency
func (builder *podEntityDTOBuilder) getNodeCPUFrequency(pod *api.Pod) (float64, error) {
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

// Build the CommodityDTOs sold  by the pod for vCPU, vMem and VMPMAcces.
// VMPMAccess is used to bind container to the hosting pod so the container is not moved out of the pod
func (builder *podEntityDTOBuilder) getPodCommoditiesSold(pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPUProvisioned)

	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(false) }, metrics.CPU, metrics.Memory)

	// Resource Commodities
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(metrics.PodType, key, podResourceCommoditySold, converter, attributeSetter)
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// vmpmAccess commodity
	podAccessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(string(pod.UID)).
		Capacity(accessCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, podAccessComm)

	return commoditiesSold, nil
}

// Build the CommodityDTOs bought by the pod from the node provider.
// Commodities bought are vCPU, vMem, cpuProvisioned, memProvisioned, access, cluster
func (builder *podEntityDTOBuilder) getPodCommoditiesBought(pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPUProvisioned)

	//// attr TODO: pod is movable, but not resizable.
	//attributeSetter := NewCommodityAttrSetter()
	//attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)

	// Resource Commodities.
	resourceCommoditiesBought, err := builder.getResourceCommoditiesBought(metrics.PodType, key, podResourceCommodityBoughtFromNode, converter, nil)
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
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(metrics.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for current Kubernetes Cluster %s", metrics.Cluster, clusterInfo)
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

// Build the CommodityDTOs bought by the pod from the quota provider.
func (builder *podEntityDTOBuilder) getPodCommoditiesBoughtFromQuota(quotaUID string, pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// cpu allocation needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 {
		return input * cpuFrequency
	}, metrics.CPULimit)

	// Resource Commodities.
	for _, resourceType := range podResourceCommodityBoughtFromQuota {
		commBought, err := builder.getResourceCommodityBoughtWithKey(metrics.PodType, key,
			resourceType, quotaUID, converter, nil)
		if err != nil {
			// skip this commodity
			glog.Errorf("%s::%s: cannot build sold commodity %s : %s",
				metrics.PodType, key, resourceType, err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
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
