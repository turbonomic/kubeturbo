package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
)

type nodeGroupEntityDTOBuilder struct {
	clusterSummary       *repository.ClusterSummary
	otherSpreadWorkloads map[string]sets.String
}

const (
	affinityCommodityDefaultCapacity = 1e10
)

func NewNodeGroupEntityDTOBuilder(clusterSummary *repository.ClusterSummary, otherSpreadWorkloads map[string]sets.String) *nodeGroupEntityDTOBuilder {
	return &nodeGroupEntityDTOBuilder{
		clusterSummary:       clusterSummary,
		otherSpreadWorkloads: otherSpreadWorkloads,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeGroupEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	nodeGrp2nodes := make(map[string]sets.String) // Map of nodeGroup ---> nodes
	for _, node := range builder.clusterSummary.Nodes {
		for key, value := range node.ObjectMeta.Labels {
			fullLabel := key + "=" + value
			nodeLst := nodeGrp2nodes[fullLabel]
			if nodeLst.Len() == 0 {
				nodeLst = sets.NewString(string(node.UID))
			} else {
				nodeLst.Insert(string(node.UID))
			}
			nodeGrp2nodes[fullLabel] = nodeLst
		}
	}
	// start building the NodeGroup entity dto
	for fullLabel, nodeLst := range nodeGrp2nodes {
		labelparts := strings.Split(fullLabel, "=")
		if len(labelparts) > 0 && labelparts[0] == "kubernetes.io/hostname" {
			continue //no need to create entity for hostname label
		}
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NODE_GROUP, fullLabel+"@"+builder.clusterSummary.Name)
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build NodeGroup entityDTO: %s", err)
			continue
		}

		// Filling in ConnectedEntity field to reflect the mapping between the nodegroup and node
		for nodeUID := range nodeLst {
			connectedEntityID := nodeUID
			connectedEntityType := proto.ConnectedEntity_NORMAL_CONNECTION
			entityDto.ConnectedEntities = append(entityDto.ConnectedEntities, &proto.ConnectedEntity{
				ConnectedEntityId: &connectedEntityID,
				ConnectionType:    &connectedEntityType,
			})
		}

		// Fill in commodities
		soldCommodities, _ := builder.getCommoditiesSold(fullLabel)
		if len(soldCommodities) > 0 {
			entityDto.CommoditiesSold = soldCommodities
		}

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}
	return result, nil
}

func (builder *nodeGroupEntityDTOBuilder) getCommoditiesSold(fullLabel string) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	//Anti-Affinity: segmentation commodity
	for tpkey, workloads := range builder.otherSpreadWorkloads {
		labelparts := strings.Split(fullLabel, "=")
		if len(labelparts) > 0 && labelparts[0] == tpkey {
			for wlname, _ := range workloads {
				glog.V(4).Infof("Add segmentation commodity with the key %v for the node_group entity %v", wlname, fullLabel)
				commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION)
				commSoldBuilder.Key(wlname)
				commSoldBuilder.Capacity(1)
				commodity, err := commSoldBuilder.Create()
				if err != nil {
					glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_SEGMENTATION, err)
					continue
				}
				commoditiesSold = append(commoditiesSold, commodity)
			}
		}
	}
	return commoditiesSold, nil

}
