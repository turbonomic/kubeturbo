package dtofactory

import (
	"fmt"
	"math"
	"strings"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
}

func NewNodeEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager) *nodeEntityDTOBuilder {
	return &nodeEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink),
		stitchingManager: stitchingManager,
	}
}

func (builder *nodeEntityDTOBuilder) WithClusterKeyInjected(clusterKeyInjected string) *nodeEntityDTOBuilder {
	builder.clusterKeyInjected = clusterKeyInjected
	return builder
}

// BuildEntityDTOs builds entityDTOs based on the given node list.
func (builder *nodeEntityDTOBuilder) BuildEntityDTOs(nodes []*api.Node, nodesPods map[string][]string,
	hostnameSpreadWorkloads sets.String, otherSpreadPods sets.String, podsToControllers map[string]string) ([]*proto.EntityDTO, []string) {
	var result []*proto.EntityDTO
	var notReadyNodes []string

	clusterId, err := builder.getClusterId()
	if err != nil {
		return result, nil
	}

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
			affinityCommoditiesSold = builder.getAffinityCommoditiesSold(node, nodesPods,
				hostnameSpreadWorkloads, otherSpreadPods, podsToControllers)
			commoditiesSold = append(commoditiesSold, affinityCommoditiesSold...)
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// entities' properties.
		properties, err := builder.getNodeProperties(node)
		if err != nil {
			glog.Errorf("Failed to get node properties: %s", err)
			nodeActive = false
		}
		for _, property := range properties{
			spot := strings.Contains(property.GetName(), util.EKSCapacityType) && property.GetValue() == util.EKSSpot
			windows := strings.Contains(property.GetName(), util.NodeLabelOS) && property.GetValue() == util.WindowsOS
			if spot || windows {
 				glog.V(2).Infof("Suspend and provision is disabled for node %s, it is either AWS EC2 spot instance or node with Windows OS.", node.GetName())
				entityDTOBuilder.IsProvisionable(false)
				entityDTOBuilder.IsSuspendable(false)
			}
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

		result = append(result, entityDto)

		glog.V(4).Infof("Node DTO : %+v", entityDto)
	}

	return result, notReadyNodes
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
			glog.Warningf("Missing capacity value for %v : %s for node %s.", resourceType, resource, nodeName)
			// If we are missing capacity its unlikely we would have other metrics either
			continue
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
			continue
		}

		resourceCommoditiesSold = append(resourceCommoditiesSold, resourceCommoditySold)
	}

	return resourceCommoditiesSold, isAvailableForPlacement
}

func (builder *nodeEntityDTOBuilder) getClusterId() (string, error) {
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(metrics.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for current Kubernetes Cluster: %v", metrics.Cluster, err)
		return "", err
	}
	clusterId, ok := clusterInfo.GetValue().(string)
	if !ok {
		glog.Error("Failed to get cluster ID")
		return "", err
	}
	return clusterId, nil
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

func (builder *nodeEntityDTOBuilder) getAffinityCommoditiesSold(node *api.Node, nodesPods map[string][]string,
	hostnameSpreadWorkloads sets.String, otherSpreadPods sets.String, podsToControllers map[string]string) []*proto.CommodityDTO {
	var commoditiesSold []*proto.CommodityDTO = nil
	// Add label commodities to honor affinities
	// This adds LABEL commodities sold for each pod that can be placed on this node
	// This also adds SEGMENTATION commodities for spread workload pods
	for _, podKey := range nodesPods[node.Name] {
		key, exists := podsToControllers[podKey]
		if !exists || otherSpreadPods.Has(podKey) {
			key = podKey
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

	for _, workloadKey := range hostnameSpreadWorkloads.UnsortedList() {
		commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
			Key(workloadKey).
			Capacity(1).
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
