package dtofactory

import (
	"fmt"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const ClusterKeyPrefix = "cluster-"

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
	for _, node := range builder.cluster.NodeList {
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
	vmGroupDTO, err := group.StaticGroup(vmGroupID).
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

func (builder *clusterDTOBuilder) BuildEntity(entityDTOs []*proto.EntityDTO) (*proto.EntityDTO, error) {
	// id.
	uid := builder.cluster.Name
	entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER, uid)

	// display name.
	displayName := builder.targetId
	entityDTOBuilder.DisplayName(displayName)

	// Resource commodities sold.
	commoditiesSold, err := builder.getCommoditiesSold(entityDTOs)
	if err != nil {
		glog.Errorf("Error creating commoditiesSold for %s: %s", builder.cluster.Name, err)
		return nil, err
	}
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	// Cluster entity cannot be provisioned or suspended by Turbonomic analysis
	entityDTOBuilder.IsProvisionable(false)
	entityDTOBuilder.IsSuspendable(false)

	entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

	// build entityDTO.
	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		glog.Errorf("Failed to build Cluster entityDTO: %s", err)
		return nil, err
	}

	glog.V(4).Infof("Cluster DTO: %+v", entityDto)
	return entityDto, nil
}

func (builder *clusterDTOBuilder) getCommoditiesSold(entityDTOs []*proto.EntityDTO) ([]*proto.CommodityDTO, error) {
	// Cluster access commodity
	clusterKey := GetClusterKey(builder.cluster.Name)
	clusterCommodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterKey).Capacity(accessCommodityDefaultCapacity).Create()
	if err != nil {
		glog.Errorf("Failed to build cluster commodity for %s: %s", builder.cluster.Name, err)
		return nil, err
	}
	commoditiesSold := []*proto.CommodityDTO{clusterCommodity}
	// Accumulate used and capacity values from the nodes on the cluster
	used := make(map[proto.CommodityDTO_CommodityType]float64)
	capacity := make(map[proto.CommodityDTO_CommodityType]float64)
	for _, entityDTO := range entityDTOs {
		if entityDTO.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE {
			for _, commodity := range entityDTO.GetCommoditiesSold() {
				// skip aggregating access commodities with keys
				if commodity.GetKey() == "" {
					commodityType := commodity.GetCommodityType()
					used[commodityType] += commodity.GetUsed()
					capacity[commodityType] += commodity.GetCapacity()
				}
			}
		}
	}

	for commodityType, capacityValue := range capacity {
		usedValue := used[commodityType]
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
	return commoditiesSold, nil
}
