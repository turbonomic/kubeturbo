package dtofactory

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	accessCommodityDefaultCapacity  = 1E10
	clusterCommodityDefaultCapacity = 1E10

	schedAccessCommodityKey string = "schedulable"
)

var (
	nodeResourceCommoditiesSold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		//metrics.CPUProvisioned,
		//metrics.MemoryProvisioned,
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
func (builder *nodeEntityDTOBuilder) BuildEntityDTOs(nodes []runtime.Object) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, node := range nodes {
		node, ok := node.(*api.Node)
		if !ok {
			glog.Warningf("%v is not a node", node.GetObjectKind())
			continue
		}

		// id.
		nodeID := string(node.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, nodeID)

		// display name.
		displayName := node.Name
		entityDTOBuilder.DisplayName(displayName)

		// commodities sold.
		commoditiesSold, err := builder.getNodeCommoditiesSold(node)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for %s: %s", node.Name, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// entities' properties.
		properties, err := builder.getNodeProperties(node)
		if err != nil {
			glog.Errorf("Failed to get node properties: %s", err)
			continue
		}
		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		// reconciliation meta data
		metaData, err := builder.stitchingManager.GenerateReconciliationMetaData()
		if err != nil {
			glog.Errorf("Failed to build reconciling metadata for node %s: %s", displayName, err)
			continue
		}
		entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

		// power state.
		entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build VM entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
	}

	return result, nil
}

// Build the sold commodityDTO by each node. They are include:
// VCPU, VMem, CPUProvisioned, MemProvisioned;
// VMPMAccessCommodity, ApplicationCommodity, ClusterCommodity.
func (builder *nodeEntityDTOBuilder) getNodeCommoditiesSold(node *api.Node) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	key := util.NodeKeyFunc(node)

	// get cpu frequency
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(task.NodeType, key, metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		glog.Errorf("Failed to get cpu frequency from sink for node %s: %s", key, err)
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	glog.Infof("Frequency is %f", cpuFrequency)
	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPUProvisioned)

	// Resource Commodities
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(task.NodeType, key, nodeResourceCommoditiesSold, converter, nil)
	if err != nil {
		return nil, err
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

	// Access commodity: schedulable.
	if probe.NodeIsReady(node) && probe.NodeIsSchedulable(node) {
		schedAccessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
			Key(schedAccessCommodityKey).
			Capacity(accessCommodityDefaultCapacity).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, schedAccessComm)
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
			Capacity(clusterCommodityDefaultCapacity).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, clusterComm)
	}

	return commoditiesSold, nil
}

// Get the properties of the node. This includes property related to stitching process and node cluster property.
func (builder *nodeEntityDTOBuilder) getNodeProperties(node *api.Node) ([]*proto.EntityDTO_EntityProperty, error) {
	var properties []*proto.EntityDTO_EntityProperty

	// stitching property.
	stitchingProperty, err := builder.stitchingManager.BuildStitchingProperty(node.Name, stitching.Reconcile)
	if err != nil {
		return nil, fmt.Errorf("failed to build properties for node %s: %s", node.Name, err)
	}
	glog.V(4).Infof("Node %s will be reconciled with VM with %s: %s", node.Name, *stitchingProperty.Name,
		*stitchingProperty.Value)
	properties = append(properties, stitchingProperty)

	// additional node cluster info property.
	nodeProperty := probe.BuildNodeProperties(node)
	properties = append(properties, nodeProperty)

	return properties, nil
}
