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

func (builder *clusterDTOBuilder) BuildEntity() (*proto.EntityDTO, error) {
	// id.
	uid := builder.cluster.Name
	entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_K8S_CLUSTER, uid)

	// display name.
	displayName := builder.targetId
	entityDTOBuilder.DisplayName(displayName)

	// Resource commodities sold.
	commoditiesSold, err := builder.getCommoditiesSold()
	if err != nil {
		glog.Errorf("Error creating commoditiesSold for %s: %s", builder.cluster.Name, err)
		return nil, err
	}
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	// Cluster entity cannot be provisioned or suspended by Turbonomic analysis
	entityDTOBuilder.IsProvisionable(false)
	entityDTOBuilder.IsSuspendable(false)

	entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

	// Link member nodes as connected entities
	for _, node := range builder.cluster.NodeList {
		entityDTOBuilder.Owns(string(node.UID))
	}

	// build entityDTO.
	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		glog.Errorf("Failed to build Cluster entityDTO: %s", err)
		return nil, err
	}

	glog.V(4).Infof("Cluster DTO: %+v", entityDto)
	return entityDto, nil
}

func (builder *clusterDTOBuilder) getCommoditiesSold() ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range builder.cluster.ClusterResources {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			continue
		}
		capacityValue := resource.Capacity
		usedValue := resource.Used
		// For CPU resources, convert the capacity and usage values expressed in
		// number of cores to MHz
		if metrics.IsCPUType(resourceType) && builder.cluster.AverageNodeCpuFrequency > 0.0 {
			if capacityValue != repository.DEFAULT_METRIC_CAPACITY_VALUE {
				// Modify the capacity value from cores to MHz if capacity is not default infinity
				newVal := capacityValue * builder.cluster.AverageNodeCpuFrequency
				glog.V(4).Infof("Changing capacity of %s::%s from %f cores to %f MHz",
					builder.targetId, resourceType, capacityValue, newVal)
				capacityValue = newVal
			}
		}

		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		commSoldBuilder.Used(usedValue)
		commSoldBuilder.Peak(usedValue)
		commSoldBuilder.Capacity(capacityValue)
		commSoldBuilder.Resizable(false)
		commSoldBuilder.Key(builder.cluster.Name)

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("%s : Failed to build commodity sold: %s", builder.cluster.Name, err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}