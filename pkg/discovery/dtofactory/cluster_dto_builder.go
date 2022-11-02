package dtofactory

import (
	"fmt"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const ClusterKeyPrefix = "cluster-"

var quotaToComputeResourceCommMap = map[proto.CommodityDTO_CommodityType]proto.CommodityDTO_CommodityType{
	proto.CommodityDTO_VCPU_LIMIT_QUOTA: proto.CommodityDTO_VCPU,
	proto.CommodityDTO_VMEM_LIMIT_QUOTA: proto.CommodityDTO_VMEM,
}

type clusterDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewClusterDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *clusterDTOBuilder {
	return &clusterDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

// GetClusterKey constructs the commodity key sold by the cluster entity, by adding a prefix to the cluster id.
// Use of a prefix is to distinguish the same key used by the node entities in the cluster
func GetClusterKey(clusterId string) string {
	return ClusterKeyPrefix + clusterId
}

func (builder *clusterDTOBuilder) BuildGroup() []*proto.GroupDTO {
	var members []string
	for _, node := range builder.cluster.Nodes {
		members = append(members, string(node.UID))
	}
	var clusterGroupDTOs []*proto.GroupDTO
	clusterGroupDTO, err := group.Cluster(builder.targetId).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(members).
		WithDisplayName(builder.targetId).
		Build()
	if err != nil {
		glog.Errorf("Failed to build cluster DTO %v", err)
	} else {
		clusterGroupDTOs = append(clusterGroupDTOs, clusterGroupDTO)
	}
	// Create a regular group of VM without cluster constraint
	// This group can be removed when the server is upgraded to 7.21.2+
	vmGroupID := fmt.Sprintf("%s-%s", builder.targetId, metrics.ClusterType)
	vmGroupDisplayName := fmt.Sprintf("%s/%s", metrics.ClusterType, builder.targetId)
	vmGroupDTO, err := group.StaticRegularGroup(vmGroupID).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(members).
		WithDisplayName(vmGroupDisplayName).
		Build()
	if err != nil {
		glog.Errorf("Failed to build VM group DTO %v", err)
	} else {
		clusterGroupDTOs = append(clusterGroupDTOs, vmGroupDTO)
	}
	return clusterGroupDTOs
}

func (builder *clusterDTOBuilder) BuildEntity(entityDTOs []*proto.EntityDTO, namespaceDTOs []*proto.EntityDTO) (*proto.EntityDTO, error) {
	// id.
	uid := builder.cluster.Name
	entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER, uid)

	// display name.
	displayName := builder.targetId
	entityDTOBuilder.DisplayName(displayName)

	// Resource commodities sold.
	commoditiesSold, nodeResourceCapacityMap, err := builder.getCommoditiesSold(entityDTOs)
	if err != nil {
		glog.Errorf("Error creating commoditiesSold for %s: %s", builder.cluster.Name, err)
		return nil, err
	}
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	// Set up ClusterData
	clusterData := builder.createClusterData(displayName, namespaceDTOs, nodeResourceCapacityMap)
	entityDTOBuilder.ClusterData(clusterData)

	// Cluster entity cannot be provisioned or suspended by Turbonomic analysis
	entityDTOBuilder.IsProvisionable(false)
	entityDTOBuilder.IsSuspendable(false)
	// Set AvailableForPlacement to false to cluster entities so that there won't be unexpected entities incorrectly
	// placed on the cluster during Turbonomic analysis.
	// Note that this flag won't affect resize actions, so this won't affect the use case in the future if we want to
	// make cluster as provider to resize namespaces.
	falseFlag := false
	entityDTOBuilder.ProviderPolicy(&proto.EntityDTO_ProviderPolicy{
		AvailableForPlacement: &falseFlag,
	})

	entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)
	// The added property indicates that this cluster now uses millicore as unit for vcpu commodities
	properties := property.BuildClusterProperty()

	// build entityDTO.
	entityDto, err := entityDTOBuilder.
		WithProperties(properties).
		Create()
	if err != nil {
		glog.Errorf("Failed to build Cluster entityDTO: %s", err)
		return nil, err
	}

	glog.V(4).Infof("Cluster DTO: %+v", entityDto)
	return entityDto, nil
}

func (builder *clusterDTOBuilder) getCommoditiesSold(entityDTOs []*proto.EntityDTO) ([]*proto.CommodityDTO,
	map[proto.CommodityDTO_CommodityType]float64, error) {
	// Cluster access commodity
	clusterKey := GetClusterKey(builder.cluster.Name)
	clusterCommodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterKey).Capacity(accessCommodityDefaultCapacity).Create()
	if err != nil {
		glog.Errorf("Failed to build cluster commodity for %s: %s", builder.cluster.Name, err)
		return nil, nil, err
	}
	commoditiesSold := []*proto.CommodityDTO{clusterCommodity}
	// Accumulate used and capacity values from the nodes on the cluster
	usedTotal := make(map[proto.CommodityDTO_CommodityType]float64)
	capacityTotal := make(map[proto.CommodityDTO_CommodityType]float64)
	for _, entityDTO := range entityDTOs {
		// Only take active nodes into account for cluster resource commodities.
		if entityDTO.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE && entityDTO.GetPowerState() == proto.EntityDTO_POWERED_ON {
			for _, commodity := range entityDTO.GetCommoditiesSold() {
				// skip aggregating access commodities with keys
				if commodity.GetKey() == "" {
					used := commodity.GetUsed()
					capacity := commodity.GetCapacity()
					commodityType := commodity.GetCommodityType()
					if commodityType == proto.CommodityDTO_VCPU {
						// We want the aggregated vCpu on cluster represented in millicores unlike nodes which we show in Mhz.
						cpuFreq := builder.cluster.GetNodeCPUFrequency(entityDTO.GetDisplayName())
						if cpuFreq > 0 {
							used = util.MetricUnitToMilli(used / cpuFreq)
							capacity = util.MetricUnitToMilli(capacity / cpuFreq)
						}
					}

					usedTotal[commodityType] += used
					capacityTotal[commodityType] += capacity
				}
			}
		}
	}

	for commodityType, capacityValue := range capacityTotal {
		usedValue := usedTotal[commodityType]
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)
		commSoldBuilder.Used(usedValue)
		commSoldBuilder.Peak(usedValue)
		commSoldBuilder.Capacity(capacityValue)
		commSoldBuilder.Resizable(false)

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("%s : Failed to build commodity sold: %s", builder.cluster.Name, err)
			continue
		}
		commoditiesSold = append(commoditiesSold, commSold)
	}
	return commoditiesSold, capacityTotal, nil
}

// Create ContainerPlatformClusterData based on total namespace quota usage (which is equivalent to total container
// resource capacity) and total node resource capacity.
func (builder *clusterDTOBuilder) createClusterData(clusterName string, namespaceDTOs []*proto.EntityDTO,
	nodeResourceCapacityMap map[proto.CommodityDTO_CommodityType]float64) *proto.EntityDTO_ContainerPlatformClusterData {
	overcommitmentUsageMap := make(map[proto.CommodityDTO_CommodityType]float64)
	for _, namespaceDTO := range namespaceDTOs {
		for _, commodity := range namespaceDTO.GetCommoditiesSold() {
			computeResourceCommType, commExists := quotaToComputeResourceCommMap[commodity.GetCommodityType()]
			if commExists {
				overcommitmentUsageMap[computeResourceCommType] += commodity.GetUsed()
			}
		}
	}

	vcpuOvercommitment := float64(0)
	vmemOvercommitment := float64(0)
	for commodityType, usage := range overcommitmentUsageMap {
		switch commodityType {
		case proto.CommodityDTO_VCPU:
			resourceCapacity := builder.getNodeResourceCapacity(clusterName, commodityType, nodeResourceCapacityMap)
			if resourceCapacity != 0 {
				vcpuOvercommitment = usage / resourceCapacity
			}
		case proto.CommodityDTO_VMEM:
			resourceCapacity := builder.getNodeResourceCapacity(clusterName, commodityType, nodeResourceCapacityMap)
			if resourceCapacity != 0 {
				vmemOvercommitment = usage / resourceCapacity
			}
		default:
			glog.Errorf("Unsupported commodity type %s for cluster overcommitment", commodityType)
		}
	}

	// Post 8.2.5 kubeturbo sends all vcpu related commodities in millicores
	vcpuCommodityUnit := proto.EntityDTO_MILLICORE
	return &proto.EntityDTO_ContainerPlatformClusterData{
		VcpuOvercommitment: &vcpuOvercommitment,
		VmemOvercommitment: &vmemOvercommitment,
		VcpuUnit:           &vcpuCommodityUnit,
	}
}

// Get node resource capacity from nodeResourceCapacityMap based on given commodityType
func (builder clusterDTOBuilder) getNodeResourceCapacity(clusterName string, commodityType proto.CommodityDTO_CommodityType,
	nodeResourceCapacityMap map[proto.CommodityDTO_CommodityType]float64) float64 {
	resourceCapacity, ok := nodeResourceCapacityMap[commodityType]
	if !ok {
		glog.Errorf("%s commodity does not exist in nodeResourceCapacityMap", commodityType)
		return 0
	}
	if resourceCapacity == 0 {
		glog.Errorf("%s commodity capacity from all nodes in cluster %s is 0.", commodityType, clusterName)
		return 0
	}
	return resourceCapacity
}
