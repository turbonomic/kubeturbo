package dtofactory

import (
	"fmt"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	accessCommodityDefaultCapacity  = 1e10
	clusterCommodityDefaultCapacity = 1e10
)

var (
	nodeResourceCommoditiesSold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
		metrics.NumPods,
		metrics.VStorage,
		// TODO, add back provisioned commodity later
	}

	allocationResourceCommoditiesSold = []metrics.ResourceType{
		metrics.CPULimitQuota,
		metrics.MemoryLimitQuota,
		metrics.CPURequestQuota,
		metrics.MemoryRequestQuota,
	}

	// List of commodities and a boolean indicating if the commodity should be resized
	resizableCommodities = map[proto.CommodityDTO_CommodityType]bool{
		proto.CommodityDTO_VCPU:         false,
		proto.CommodityDTO_VMEM:         false,
		proto.CommodityDTO_VCPU_REQUEST: false,
		proto.CommodityDTO_VMEM_REQUEST: false,
	}
)

type nodeEntityDTOBuilder struct {
	generalBuilder
	stitchingManager *stitching.StitchingManager
}

func NewNodeEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager) *nodeEntityDTOBuilder {
	return &nodeEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink),
		stitchingManager: stitchingManager,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeEntityDTOBuilder) BuildEntityDTOs(nodes []*api.Node,
	podsToVolsMap map[string][]repository.NodeVolume) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, node := range nodes {
		// id.
		nodeID := string(node.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, nodeID)

		// display name.
		displayName := node.Name
		entityDTOBuilder.DisplayName(displayName)
		nodeActive := util.NodeIsReady(node)
		if !nodeActive {
			glog.Warningf("the NodeIsReady marked node %s as inactive", node.Name)
		}

		// compute and constraint commodities sold.
		commoditiesSold, err := builder.getNodeCommoditiesSold(node)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for node %s: %s", node.Name, err)
			nodeActive = false
		}
		// allocation commodities sold
		allocationCommoditiesSold, err := builder.getAllocationCommoditiesSold(node)
		if err != nil {
			glog.Errorf("Error when creating allocation commoditiesSold for node %s: %s", node.Name, err)
			nodeActive = false
		}
		commoditiesSold = append(commoditiesSold, allocationCommoditiesSold...)
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		err = builder.buyCommoditiesFromVolumes(displayName, podsToVolsMap[displayName], entityDTOBuilder)
		if err != nil {
			glog.Errorf("Error when create commoditiesBought for node %s: %s", displayName, err)
		}

		// entities' properties.
		properties, err := builder.getNodeProperties(node)
		if err != nil {
			glog.Errorf("Failed to get node properties: %s", err)
			nodeActive = false
		}
		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		// reconciliation meta data
		metaData, err := builder.stitchingManager.GenerateReconciliationMetaData()
		if err != nil {
			glog.Errorf("Failed to build reconciling metadata for node %s: %s", displayName, err)
			nodeActive = false
		}
		entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)
		// Check whether we have used cache
		cacheUsedMetric := metrics.GenerateEntityStateMetricUID(metrics.NodeType, util.NodeKeyFunc(node), "NodeCacheUsed")
		present, _ := builder.metricsSink.GetMetric(cacheUsedMetric)
		if present != nil {
			glog.Errorf("We have used the cached data, so the node %s appeared to be flaky", displayName)
			nodeActive = false
		}

		controllable := util.NodeIsControllable(node)
		entityDTOBuilder = entityDTOBuilder.ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
			Controllable: &controllable,
		})

		// Action settings for a node
		// Allow suspend for all nodes except those marked as HA via kubeturbo config
		isHANode := util.DetectHARole(node)
		entityDTOBuilder.IsSuspendable(!isHANode)

		// Power state.
		// Will be Powered On, only if it is ready and has no issues with kubelet accessibility.
		if nodeActive {
			entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)
		} else {
			glog.Warningf("Node %s has unknown power state", node.GetName())
			entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERSTATE_UNKNOWN)
		}

		vmdata := &proto.EntityDTO_VirtualMachineData{
			IpAddress: getNodeIPs(node),
		}
		entityDTOBuilder = entityDTOBuilder.VirtualMachineData(vmdata)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build VM entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)

		glog.V(3).Infof("node dto : %++v\n", entityDto)
	}

	return result, nil
}

// Build the CommodityDTOs bought by the node from the Volumes.
func (builder *nodeEntityDTOBuilder) buyCommoditiesFromVolumes(nodeName string, volumes []repository.NodeVolume, dtoBuilder *sdkbuilder.EntityDTOBuilder) error {
	// TODO: Add a check to see if same volume is being used by multiple
	// pods here to ensure no duplicate commodities bought are added.
	for _, v := range volumes {
		mountName := v.MountName
		podKey := v.QualifiedPodName
		volEntityID := util.PodVolumeMetricId(podKey, mountName)
		commBought, err := builder.getResourceCommodityBoughtWithKey(metrics.VolumeType, volEntityID,
			metrics.StorageAmount, "", nil, nil)
		if err != nil {
			glog.Errorf("Failed to build %s bought by node %s and mounted as %s from volume %s: %v",
				metrics.StorageAmount, nodeName, mountName, v.UsedVolume.Name, err)
			return err
		}

		if v.UsedVolume == nil {
			glog.Errorf("Error when create commoditiesBought for node %s mounting %s: Cannot find uuid for provider "+
				"Vol: ", podKey, mountName)
			continue
		}

		providerVolUID := string(v.UsedVolume.UID)

		provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_VOLUME, providerVolUID)
		dtoBuilder = dtoBuilder.Provider(provider)
		// TODO: Clear this out with the cloud storage team.
		// We do not expect any vm actions related to volumes as provider.
		dtoBuilder.IsMovable(proto.EntityDTO_VIRTUAL_VOLUME, false).
			IsStartable(proto.EntityDTO_VIRTUAL_VOLUME, false).
			IsScalable(proto.EntityDTO_VIRTUAL_VOLUME, false)
		dtoBuilder.BuysCommodities([]*proto.CommodityDTO{commBought})
	}

	return nil
}

// Build the sold commodityDTO by each node. They include:
// VCPU, VMem, CPURequest, MemRequest;
// VMPMAccessCommodity, ApplicationCommodity, ClusterCommodity.
func (builder *nodeEntityDTOBuilder) getNodeCommoditiesSold(node *api.Node) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	// get cpu frequency
	key := util.NodeKeyFunc(node)
	cpuFrequency, err := builder.getNodeCPUFrequency(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get cpu frequency from sink for node %s: %s", key, err)
	}
	// cpu and cpu request needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(
		func(input float64) float64 {
			return input * cpuFrequency
		},
		metrics.CPU, metrics.CPURequest)

	// Resource Commodities
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(metrics.NodeType, key, nodeResourceCommoditiesSold, converter, nil)
	if err != nil {
		return nil, err
	}

	// Disable vertical resize of the resource commodities for all nodes
	for _, commSold := range resourceCommoditiesSold {
		if isResizeable, exists := resizableCommodities[commSold.GetCommodityType()]; exists {
			commSold.Resizable = &isResizeable
		}
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// Access commodities: labels.
	for key, value := range node.ObjectMeta.Labels {
		label := key + "=" + value
		glog.V(4).Infof("label for this Node is : %s", label)

		accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
			Key(label).
			Capacity(accessCommodityDefaultCapacity).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, accessComm)
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
			Capacity(clusterCommodityDefaultCapacity).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, clusterComm)
	}

	return commoditiesSold, nil
}

func (builder *nodeEntityDTOBuilder) getAllocationCommoditiesSold(node *api.Node) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	// get cpu frequency
	key := util.NodeKeyFunc(node)
	cpuFrequency, err := builder.getNodeCPUFrequency(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get cpu frequency from sink for node %s: %s", key, err)
	}
	// cpuLimitQuota and cpuRequestQuota needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(
		func(input float64) float64 {
			return input * cpuFrequency
		},
		metrics.CPULimitQuota, metrics.CPURequestQuota)

	// Resource Commodities
	var resourceCommoditiesSold []*proto.CommodityDTO
	for _, resourceType := range allocationResourceCommoditiesSold {
		commSold, _ := builder.getSoldResourceCommodityWithKey(metrics.NodeType, key, resourceType, string(node.UID),
			converter, nil)
		if commSold != nil {
			resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
		}
	}

	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)
	return commoditiesSold, nil
}

// Get the properties of the node. This includes property related to stitching process and node cluster property.
func (builder *nodeEntityDTOBuilder) getNodeProperties(node *api.Node) ([]*proto.EntityDTO_EntityProperty, error) {
	var properties []*proto.EntityDTO_EntityProperty

	// stitching property.
	isForReconcile := true
	stitchingProperty, err := builder.stitchingManager.BuildDTOProperty(node.Name, isForReconcile)
	if err != nil {
		return nil, fmt.Errorf("failed to build properties for node %s: %s", node.Name, err)
	}
	glog.V(4).Infof("Node %s will be reconciled with VM with %s: %s", node.Name, *stitchingProperty.Name,
		*stitchingProperty.Value)
	properties = append(properties, stitchingProperty)

	// additional node info properties.
	properties = append(properties, property.BuildNodeProperties(node)...)
	return properties, nil
}

func getNodeIPs(node *api.Node) []string {
	result := []string{}

	addrs := node.Status.Addresses
	for i := range addrs {
		result = append(result, addrs[i].Address)
	}
	return result
}
