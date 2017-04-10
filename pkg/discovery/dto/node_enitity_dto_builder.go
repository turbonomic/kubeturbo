package dto

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	accessCommodityDefaultCapacity      = 1E10
	applicationCommodityDefaultCapacity = 1E10
	clusterCommodityDefaultCapacity     = 1E10

	proxyVMIP string = "Proxy_VM_IP"
)

var (
	nodeResourceCommoditySold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPUProvisioned,
		metrics.MemoryProvisioned,
	}
)

type NodeEntityDTOBuilder struct {
	generalBuilder
}

// Build entityDTOs based on the given node list.
func (builder *NodeEntityDTOBuilder) buildEntityDTO(nodes []*api.Node) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, node := range nodes {
		// We do not parse node that is not ready or unschedulable.
		if !nodeIsReady(node) || !nodeIsSchedulable(node) {
			continue
		}

		nodeID := string(node.UID)
		dispName := node.Name

		commoditiesSold, err := builder.getNodeCommoditiesSold(node)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for %s: %s", node.Name, err)
			continue
		}
		entityDto, err := builder.buildVMEntityDTO(nodeID, dispName, commoditiesSold)
		if err != nil {
			return nil, err
		}

		result = append(result, entityDto)
	}

	return result, nil
}

// Build the sold commodityDTO by each node. They are include:
// VCPU, VMem, CPUProvisioned, MemProvisioned;
// VMPMAccessCommodity, ApplicationCommodity, ClusterCommodity.
func (builder *NodeEntityDTOBuilder) getNodeCommoditiesSold(node *api.Node) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	key := util.NodeKeyFunc(&node)

	// Resource Commodities
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(task.NodeType, key, nodeResourceCommoditySold)
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// VMPM_ACCESS
	labelMetricUID := metrics.GenerateEntityStateMetricUID(task.NodeType, key, metrics.Access)
	labelMetric, err := builder.metricsSink.GetMetric(labelMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for %s %s", metrics.Access, task.NodeType, key)
	} else {
		labelPairs, ok := labelMetric.GetValue().([]string)
		if !ok {
			glog.Errorf("Failed to get label pairs for %s %s", task.NodeType, key)
		}
		for _, label := range labelPairs {
			accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(label).
				Capacity(accessCommodityDefaultCapacity).
				Create()
			if err != nil {
				return nil, err
			}

			commoditiesSold = append(commoditiesSold, accessComm)
		}
	}

	// APPLICATION
	appComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(key).
		Capacity(applicationCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, appComm)

	// CLUSTER
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

// Build EntityDTO for a single node.
func (nodeProbe *NodeEntityDTOBuilder) buildVMEntityDTO(nodeID, displayName string, commoditiesSold []*proto.CommodityDTO) (*proto.EntityDTO, error) {
	entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, nodeID)
	entityDTOBuilder.DisplayName(displayName)
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	ipAddress := getNodeIPAddress(displayName)
	propertyName := proxyVMIP

	// TODO, make it a constant.
	propertyNamespace := "DEFAULT"
	entityDTOBuilder = entityDTOBuilder.WithProperty(&proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &ipAddress,
	})
	glog.V(4).Infof("Parse node: The ip of vm to be reconcile with is %s", ipAddress)

	metaData := generateReconciliationMetaData()
	entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

	entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for node %s: %s", nodeID, err)
	}

	return entityDto, nil
}

// Retrieve the IP address of each node for stitching.
// This IP address should be the same IP address discovery by the other probe, such as hypervisor probe or cloud probe.
func getNodeIPAddress(node *api.Node) string {
	var ip string
	for _, addr := range node.Status.Addresses {
		if addr.Type == api.NodeExternalIP && addr.Address != "" {
			ip = addr.Address
		}
		if addr.Type == api.NodeInternalIP && addr.Address != "" && ip == "" {
			ip = addr.Address
		}
		if addr.Type == api.NodeLegacyHostIP && addr.Address != "" && ip == "" {
			ip = addr.Address
		}

	}
	return ip
}

// Create the meta data that will be used during the reconciliation process.
func generateReconciliationMetaData() *proto.EntityDTO_ReplacementEntityMetaData {
	replacementEntityMetaDataBuilder := sdkbuilder.NewReplacementEntityMetaDataBuilder()
	replacementEntityMetaDataBuilder.Matching(proxyVMIP)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_CPU_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_MEM_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_APPLICATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_VMPM_ACCESS)

	metaData := replacementEntityMetaDataBuilder.Build()
	return metaData
}

// Check is a node is ready.
func nodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			if condition.Status == api.ConditionTrue {
				return true
			}
		}
	}
	glog.V(1).Infof("Node %s is not Ready.", node.Name)
	return false
}

// Check if a node is schedulable.
func nodeIsSchedulable(node *api.Node) bool {
	if node.Spec.Unschedulable {
		glog.V(1).Infof("Node %s does not have Ready status.", node.Name)
		return false
	}
	return true
}
