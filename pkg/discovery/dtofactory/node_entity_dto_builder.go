package dtofactory

import (
	"fmt"
	"math"
	"strings"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	accessCommodityDefaultCapacity  = 1e10
	clusterCommodityDefaultCapacity = 1e10
	labelCommodityDefaultCapacity   = 1e10
)

var (
	nodeResourceCommoditiesSold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
		metrics.NumPods,
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
	stitchingManager   *stitching.StitchingManager
	clusterKeyInjected string
	affinityMapper     *podaffinity.AffinityMapper
}

func NewNodeEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager,
	clusterSummary *repository.ClusterSummary, affinityMapper *podaffinity.AffinityMapper) *nodeEntityDTOBuilder {
	return &nodeEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink, clusterSummary),
		stitchingManager: stitchingManager,
		affinityMapper:   affinityMapper,
	}
}

func (builder *nodeEntityDTOBuilder) WithClusterKeyInjected(clusterKeyInjected string) *nodeEntityDTOBuilder {
	builder.clusterKeyInjected = clusterKeyInjected
	return builder
}

// BuildEntityDTOs builds entityDTOs based on the given node list.
func (builder *nodeEntityDTOBuilder) BuildEntityDTOs(nodes []*api.Node, nodesPods map[string][]string,
	hostnameSpreadPods, hostnameSpreadWorkloads, otherSpreadPods sets.String,
	podsToControllers map[string]string, node2nodegroup map[string]sets.String) ([]*proto.EntityDTO, []string) {
	var result []*proto.EntityDTO
	var notReadyNodes []string

	clusterId := builder.clusterSummary.Name
	for _, node := range nodes {
		// id.
		nodeID := string(node.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, nodeID)

		// display name.
		displayName := node.Name
		entityDTOBuilder.DisplayName(displayName)
		nodeActive := util.NodeIsReady(node)
		if !nodeActive {
			glog.Warningf("Node %s is in NotReady status.", node.Name)
		}

		// compute and constraint commodities sold.
		commoditiesSold, isAvailableForPlacement, err := builder.getNodeCommoditiesSold(node, clusterId)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for %s: %s", node.Name, err)
			nodeActive = false
		}
		// allocation commodities sold
		allocationCommoditiesSold, err := builder.getAllocationCommoditiesSold(node)
		if err != nil {
			glog.Errorf("Error when creating allocation commoditiesSold for %s: %s", node.Name, err)
			nodeActive = false
		}

		commoditiesSold = append(commoditiesSold, allocationCommoditiesSold...)
		// affinity commodities sold
		var affinityCommoditiesSold []*proto.CommodityDTO
		if utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) {
			affinityCommoditiesSold = builder.getAffinityLabelAndSegmentationComms(node,
				nodesPods, hostnameSpreadPods, hostnameSpreadWorkloads, podsToControllers)
			commoditiesSold = append(commoditiesSold, affinityCommoditiesSold...)
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

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
		nodeKey := util.NodeKeyFunc(node)
		cacheUsedMetric := metrics.GenerateEntityStateMetricUID(metrics.NodeType, nodeKey, "NodeCacheUsed")
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

		disableSuspendProvision, nodeType := getSuspendProvisionSettingByNodeType(properties)
		if disableSuspendProvision {
			glog.V(2).Infof("Suspend and provision is disabled for node %s, it is a %s", node.GetName(), nodeType)
			entityDTOBuilder.IsProvisionable(false)
			entityDTOBuilder.IsSuspendable(false)
		}

		if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
			// we force the node status as active, even if we did not find metrics for it
			nodeActive = true
		}

		if !nodeActive {
			glog.Warningf("Node %s has NotReady status or has issues accessing kubelet.", node.GetName())
			notReadyNodes = append(notReadyNodes, nodeID)
			entityDTOBuilder.IsSuspendable(false)
			entityDTOBuilder.IsProvisionable(false)
			clusterCommodityKey := fmt.Sprintf("Node-%v-NotReady", nodeID)
			clusterComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
				Key(clusterCommodityKey).
				Used(1).
				Create()
			if err == nil {
				provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER, clusterId)
				entityDTOBuilder.
					Provider(provider).
					BuysCommodity(clusterComm)
			}
		}

		// Get CPU capacity in cores.
		cpuMetricValue, err := builder.metricValue(metrics.NodeType, nodeKey, metrics.CPU, metrics.Capacity, nil)
		if err != nil {
			glog.Errorf("Failed to get number of CPU in cores for VM %s: %v", nodeKey, err)
			continue
		}
		cpuCores := int32(math.Round(util.MetricMilliToUnit(cpuMetricValue.Avg)))
		vmdata := &proto.EntityDTO_VirtualMachineData{
			IpAddress: getNodeIPs(node),
			// Set numCPUs in cores.
			NumCpus: &cpuCores,
		}
		entityDTOBuilder = entityDTOBuilder.VirtualMachineData(vmdata)

		// also set up the aggregatedBy relationship with the cluster
		entityDTOBuilder.AggregatedBy(clusterId)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build VM entityDTO: %s", err)
			continue
		}

		if !isAvailableForPlacement {
			glog.Warningf("Node %s has been marked unavailable for placement due to disk pressure.", node.GetName())
		}
		nodeSchedulable := nodeActive && util.NodeIsSchedulable(node)
		if !nodeSchedulable {
			glog.Warningf("Node %s has been marked unavailable for placement because its Unschedulable.", node.GetName())
		}
		isAvailableForPlacement = isAvailableForPlacement && nodeSchedulable
		entityDto.ProviderPolicy = &proto.EntityDTO_ProviderPolicy{AvailableForPlacement: &isAvailableForPlacement}

		// Connect node to nodegroup
		// node2nodegroups will be empty if SegmentationBasedTopologySpread is not enabled
		if nodegrps, ok := node2nodegroup[nodeID]; ok {
			for nodegrp := range nodegrps {
				connectedEntityID := nodegrp
				connectedEntityType := proto.ConnectedEntity_NORMAL_CONNECTION
				entityDto.ConnectedEntities = append(entityDto.ConnectedEntities, &proto.ConnectedEntity{
					ConnectedEntityId: &connectedEntityID,
					ConnectionType:    &connectedEntityType,
				})
			}
		}

		result = append(result, entityDto)
		glog.V(4).Infof("Node DTO : %+v", entityDto)
	}

	return result, notReadyNodes
}

func (builder *nodeEntityDTOBuilder) buildAffinityCommodity(key string, used float64) (*proto.CommodityDTO, error) {
	commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY).
		Key(key).
		Used(used).
		Capacity(affinityCommodityDefaultCapacity).
		Create()
	if err != nil {
		glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_AFFINITY, err)
		return nil, err
	}
	return commodityDTO, err
}

func (builder *nodeEntityDTOBuilder) buildAffinityCommodities(node *api.Node) ([]*proto.CommodityDTO, error) {
	commoditiesSold := []*proto.CommodityDTO{}
	soldAffinityKeys := sets.Set[string]{}

	// Generate commodities for workloads for which the node is currently a provider
	if providerMapping, exists := builder.affinityMapper.GetProviderMapping(node.Name); exists {
		for key := range providerMapping.Keys.AffinityKeys {
			used := 0.0
			if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AffinitySrc}]; exists {
				used += float64(podList.Len()) * podaffinity.NONE
			}
			if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AffinityDst}]; exists {
				used += float64(podList.Len())
			}
			affinityComm, err := builder.buildAffinityCommodity(key, used)
			if err != nil {
				return nil, err
			}
			commoditiesSold = append(commoditiesSold, affinityComm)
			soldAffinityKeys.Insert(key)
		}
	}

	// Add commodities for workloads for which this node is not a provider
	soldKeys := builder.affinityMapper.GetNodeSoldKeys()
	for key := range soldKeys.AffinityKeys {
		if soldAffinityKeys.Has(key) {
			continue
		}
		commodity, err := builder.buildAffinityCommodity(key, 0)
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, commodity)
	}

	return commoditiesSold, nil
}

func (builder *nodeEntityDTOBuilder) buildAntiAffinityCommodity(key string, used float64, peak float64) (*proto.CommodityDTO, error) {
	commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY).
		Key(key).
		Used(used).
		Peak(peak).
		Capacity(affinityCommodityDefaultCapacity).
		Create()
	if err != nil {
		glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY, err)
		return nil, err
	}
	return commodityDTO, nil
}

func (builder *nodeEntityDTOBuilder) buildAntiAffinityCommodities(node *api.Node) ([]*proto.CommodityDTO, error) {
	commoditiesSold := []*proto.CommodityDTO{}
	soldAntiAffinityKeys := sets.Set[string]{}

	// Generate commodities for workloads for which the node is currently a provider
	if providerMapping, exists := builder.affinityMapper.GetProviderMapping(node.Name); exists {
		for key := range providerMapping.Keys.AntiAffinityKeys {
			used := 0.0
			peak := 0.0
			if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AntiAffinitySrc}]; exists {
				used += float64(podList.Len())
				peak += float64(podList.Len()) * podaffinity.NONE
			}
			if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AntiAffinityDst}]; exists {
				used += float64(podList.Len()) * podaffinity.NONE
				peak += float64(podList.Len())
			}
			antiAffinityComm, err := builder.buildAntiAffinityCommodity(key, used, peak)
			if err != nil {
				return nil, err
			}
			commoditiesSold = append(commoditiesSold, antiAffinityComm)
			soldAntiAffinityKeys.Insert(key)
		}
	}

	// Add commodities for workloads for which this node is not a provider
	soldKeys := builder.affinityMapper.GetNodeSoldKeys()
	for key := range soldKeys.AntiAffinityKeys {
		if soldAntiAffinityKeys.Has(key) {
			continue
		}
		commodity, err := builder.buildAntiAffinityCommodity(key, 0, 0)
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, commodity)
	}

	return commoditiesSold, nil
}

// Build the sold commodityDTO by each node. They include:
// VCPU, VMem, CPURequest, MemRequest;
// VMPMAccessCommodity, ApplicationCommodity, ClusterCommodity.
func (builder *nodeEntityDTOBuilder) getNodeCommoditiesSold(node *api.Node, clusterId string) ([]*proto.CommodityDTO, bool, error) {
	var commoditiesSold []*proto.CommodityDTO
	// get cpu frequency
	key := util.NodeKeyFunc(node)
	cpuFrequency, err := builder.getNodeCPUFrequency(key)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get cpu frequency from sink for node %s: %s", key, err)
	}
	// cpu and cpu request needs to be converted from number of millicores to frequency.
	converter := NewConverter().Set(
		func(input float64) float64 {
			// All cpu metrics are stored in millicores in metrics sink for consistency
			// But we send the node cpu metrics in MHz.
			return util.MetricMilliToUnit(input) * cpuFrequency
		},
		metrics.CPU)

	// Resource Commodities
	resourceCommoditiesSold := builder.getResourceCommoditiesSold(metrics.NodeType, key, nodeResourceCommoditiesSold, converter, nil)
	storageCommoditiesSold, isAvailableForPlacement := builder.getNodeStorageCommoditiesSold(node.Name)
	resourceCommoditiesSold = append(resourceCommoditiesSold, storageCommoditiesSold...)

	// Disable vertical resize of the resource commodities for all nodes
	for _, commSold := range resourceCommoditiesSold {
		if isResizeable, exists := resizableCommodities[commSold.GetCommodityType()]; exists {
			commSold.Resizable = &isResizeable
		}
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// Label commodities
	for key, value := range node.ObjectMeta.Labels {
		label := key + "=" + value
		labelComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_LABEL).
			Key(label).
			Capacity(labelCommodityDefaultCapacity).
			Create()
		if err != nil {
			return nil, isAvailableForPlacement, err
		}
		glog.V(5).Infof("Adding label commodity for Node %s with key : %s", node.Name, label)
		commoditiesSold = append(commoditiesSold, labelComm)
	}

	// Cluster commodity.
	var clusterCommKey string
	if len(strings.TrimSpace(builder.clusterKeyInjected)) != 0 {
		clusterCommKey = builder.clusterKeyInjected
		glog.V(4).Infof("Injecting cluster key for Node %s with key : %s", node.Name, clusterCommKey)
	} else {
		clusterCommKey = clusterId
		glog.V(4).Infof("Adding cluster key for Node %s with key : %s", node.Name, clusterCommKey)
	}
	clusterComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterCommKey).
		Capacity(clusterCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, isAvailableForPlacement, err
	}
	commoditiesSold = append(commoditiesSold, clusterComm)

	affinityCommodities, err := builder.buildAffinityCommodities(node)
	if err != nil {
		return nil, isAvailableForPlacement, err
	}
	commoditiesSold = append(commoditiesSold, affinityCommodities...)

	antiAffinityCommodities, err := builder.buildAntiAffinityCommodities(node)
	if err != nil {
		return nil, isAvailableForPlacement, err
	}
	commoditiesSold = append(commoditiesSold, antiAffinityCommodities...)

	return commoditiesSold, isAvailableForPlacement, nil
}

// getNodeStorageCommoditiesSold builds sold storage commodities for the node
// Returns the built commodities and if this node is available for placement or not.
// The availability for placement is evaluated based on the current usage crossing the
// configured threshold. If the usage has crossed the threshold, we mark the node
// NOT available for placement.
func (builder *nodeEntityDTOBuilder) getNodeStorageCommoditiesSold(nodeName string) ([]*proto.CommodityDTO, bool) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	vstorageResources := []string{"rootfs", "imagefs"}
	var rootfsCapacityBytes, rootfsAvailableBytes float64
	entityType := metrics.NodeType
	resourceType := metrics.VStorage
	protoCommodityType := proto.CommodityDTO_VSTORAGE
	isAvailableForPlacement := true
	previousThreshold := float64(0)

	for _, resource := range vstorageResources {
		entityID := nodeName
		if resource == "imagefs" {
			entityID = fmt.Sprintf("%s-%s", nodeName, resource)
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(protoCommodityType)

		// set capacity value
		capacityBytes, err := builder.metricValue(metrics.NodeType, entityID,
			metrics.VStorage, metrics.Capacity, nil)
		if err != nil || (capacityBytes.Avg == 0 && capacityBytes.Peak == 0) {
			if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
				capacityBytes = metrics.MetricValue{Avg: 100000, Peak: 100000}
			} else {
				glog.Warningf("Missing capacity value for %v : %s for node %s.", resourceType, resource, nodeName)
				// If we are missing capacity its unlikely we would have other metrics either
				continue
			}
		}

		if resource == "rootfs" {
			// We iterate the vstorageResources slice in order so the rootfs values
			// are always preserved in the first pass of this loop.
			rootfsCapacityBytes = capacityBytes.Avg
		}
		// Capacity metric is always a single data point. Use Avg to refer to the single point value
		commSoldBuilder.Capacity(util.Base2BytesToMegabytes(capacityBytes.Avg))

		usedBytes := float64(0)
		availableBytes, err := builder.metricValue(metrics.NodeType, entityID,
			metrics.VStorage, metrics.Available, nil)
		if err != nil {
			glog.Warningf("Missing used value for %v : %s for node %s.", resourceType, resource, nodeName)
		} else {
			if resource == "rootfs" {
				rootfsAvailableBytes = availableBytes.Avg
			}
			usedBytes = capacityBytes.Avg - availableBytes.Avg
			commSoldBuilder.Used(util.Base2BytesToMegabytes(usedBytes))
			commSoldBuilder.Peak(util.Base2BytesToMegabytes(usedBytes))
		}

		// set commodity key
		commSoldBuilder.Key(fmt.Sprintf("k8s-node-%s", resource))
		resourceCommoditySold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Warning(err.Error())
			continue
		}

		threshold, err := builder.metricValue(entityType, entityID,
			resourceType, metrics.Threshold, nil)
		if err != nil {
			glog.Warningf("Missing threshold value for %v for node %s.", resourceType, nodeName)
		} else {

			if threshold.Avg > 0 && threshold.Avg <= 100 {
				if resource == "rootfs" {
					previousThreshold = threshold.Avg
				}
				isAvailableAboveThreshold := availableBytes.Avg > threshold.Avg*capacityBytes.Avg/100
				isAvailableForPlacement = isAvailableAboveThreshold
				utilizationThreshold := 100 - threshold.Avg
				// TODO: The settable method for UtilizationThresholdPct can be added to the sdk instead.
				resourceCommoditySold.UtilizationThresholdPct = &utilizationThreshold
			} else {
				glog.Warningf("Threshold value [%.2f] outside range and will not be set for %v : %s for node %s.",
					threshold.Avg, resourceType, resource, nodeName)
			}
		}

		// We currently have no way of knowing the command line configuration of kubelet
		// to understand if there is a separate imagefs partition configured. We use the workaround
		// comparing the reported capacity and available bytes, to the last byte, for both
		// rootfs and imagefs to determine if we are getting the reported values for the same partition.
		isPartitionSame := resource == "imagefs" && rootfsCapacityBytes == capacityBytes.Avg && rootfsAvailableBytes == availableBytes.Avg
		if isPartitionSame {
			// We skip adding imagefs commodity, however we still honor the thresholds set for imagefs
			// which can be different compared to rootfs, even when the partitions are same.
			// isAvailableForPlacement is still calculated for both above and would be set to false
			// if either of the values cross threshold.
			if threshold.Avg > previousThreshold {
				prevResourceCommoditySold := resourceCommoditiesSold[len(resourceCommoditiesSold)-1]
				utilThreshold := 100 - threshold.Avg
				prevResourceCommoditySold.UtilizationThresholdPct = &utilThreshold
			} else {
				continue
			}
		} else {
			resourceCommoditiesSold = append(resourceCommoditiesSold, resourceCommoditySold)
		}
	}

	return resourceCommoditiesSold, isAvailableForPlacement
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

func (builder *nodeEntityDTOBuilder) getAffinityLabelAndSegmentationComms(node *api.Node, nodesPods map[string][]string,
	hostnameSpreadPods, hostnameSpreadWorkloads sets.String, podsToControllers map[string]string) []*proto.CommodityDTO {
	var commoditiesSold []*proto.CommodityDTO = nil
	// Add label commodities to honor affinities
	// This adds LABEL commodities sold for each pod that can be placed on this node
	// This also adds SEGMENTATION commodities for spread workload pods
	for _, key := range nodesPods[node.Name] {
		// We rely on what shows up in nodespods to add LABELs, because that is supposed to be
		// filled appropriately, taking care of all feature flags and relevant conditions
		controllerId, exists := podsToControllers[key]
		if exists {
			// we use parent controller id as the key if we have one
			// if not the pod probably is a standalone pod
			key = controllerId
		}
		commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_LABEL).
			Key(key).
			Capacity(accessCommodityDefaultCapacity).
			Create()
		if err != nil {
			glog.Warningf("Error creating LABEL sold commodity for key %s on node %s", key, node.Name)
			// We ignore a failure and continue to add the rest
			continue
		}
		commoditiesSold = append(commoditiesSold, commodityDTO)
	}

	segmentationCommodityUsage := make(map[string]float64)
	allPodsOnThisNode := getAllPodsOnNode(node, builder.clusterSummary)
	for _, podKey := range hostnameSpreadPods.UnsortedList() {
		used := 0.0
		if allPodsOnThisNode.Has(podKey) {
			used = 1.0
		}
		if workloadKey, exists := builder.clusterSummary.PodToControllerMap[podKey]; exists {
			if _, exists := segmentationCommodityUsage[workloadKey]; !exists {
				segmentationCommodityUsage[workloadKey] = 0.0
			}
			segmentationCommodityUsage[workloadKey] = segmentationCommodityUsage[workloadKey] + used
		}
	}

	for _, workloadKey := range hostnameSpreadWorkloads.UnsortedList() {
		used := segmentationCommodityUsage[workloadKey]
		commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
			Key(workloadKey).
			Capacity(1).
			Used(used).
			Create()
		if err != nil {
			glog.Warningf("Error creating SEGMENTATION sold commodity for key %s on node %s", workloadKey, node.Name)
			// We ignore a failure and continue to add the rest
			continue
		}
		commoditiesSold = append(commoditiesSold, commodityDTO)

	}

	return commoditiesSold
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
	properties = append(properties, property.BuildNodeProperties(node, builder.clusterSummary.Name)...)
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

func getSuspendProvisionSettingByNodeType(properties []*proto.EntityDTO_EntityProperty) (disableSuspendProvision bool, nodeType string) {
	disableSuspendProvision = false
	for _, property := range properties {
		spot := strings.Contains(property.GetName(), util.EKSCapacityType) && property.GetValue() == util.EKSSpot
		windows := strings.Contains(property.GetName(), util.NodeLabelOS) && property.GetValue() == util.WindowsOS
		if spot || windows {
			if spot {
				nodeType = "AWS EC2 spot instance"
			} else if windows {
				nodeType = "node with Windows OS"
			}
			disableSuspendProvision = true
			return
		}
	}
	return
}

// getAllWorkloadsOnNode retrieves the controller names for all pods on a node.
// It takes a node and a ClusterSummary as input and returns a set of controller names.
func getAllWorkloadsOnNode(node *api.Node, clusterSummary *repository.ClusterSummary) sets.String {
	controllerNames := sets.NewString()
	allPods := append(clusterSummary.GetPendingPodsOnNode(node), clusterSummary.GetRunningPodsOnNode(node)...)

	for _, pod := range allPods {
		podQualifiedName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		if ctrlName, exists := clusterSummary.PodToControllerMap[podQualifiedName]; exists {
			controllerNames.Insert(ctrlName)
		}
	}
	return controllerNames
}

func getAllPodsOnNode(node *api.Node, clusterSummary *repository.ClusterSummary) sets.String {
	podNames := sets.NewString()
	allPods := append(clusterSummary.GetPendingPodsOnNode(node), clusterSummary.GetRunningPodsOnNode(node)...)
	for _, pod := range allPods {
		podNames.Insert(pod.Namespace + "/" + pod.Name)
	}
	return podNames
}
