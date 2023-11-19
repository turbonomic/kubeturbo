package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
)

type nodeGroupEntityDTOBuilder struct {
	clusterSummary          *repository.ClusterSummary
	otherSpreadWorkloads    map[string]sets.String
	otherSpreadTopologyKeys sets.String
}

const (
	affinityCommodityDefaultCapacity = 1e10
)

func NewNodeGroupEntityDTOBuilder(clusterSummary *repository.ClusterSummary, otherSpreadWorkloads map[string]sets.String,
	otherSpreadTopologyKeys sets.String) *nodeGroupEntityDTOBuilder {
	return &nodeGroupEntityDTOBuilder{
		clusterSummary:          clusterSummary,
		otherSpreadWorkloads:    otherSpreadWorkloads,
		otherSpreadTopologyKeys: otherSpreadTopologyKeys,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeGroupEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, map[string]sets.String) {
	var result []*proto.EntityDTO
	node2nodeGrp := make(map[string]sets.String)      // Map of node ---> NodeGroup
	nodeGrp2nodes := make(map[string]sets.String)     // Map of nodeGroup ---> nodes
	nodeGrp2workloads := make(map[string]sets.String) // Map of nodeGroup ---> workloads of all pods on each node of the nodegroup
	for _, node := range builder.clusterSummary.Nodes {
		allWorkloadsOnNode := getAllWorkloadsOnNode(node, builder.clusterSummary)
		for key, value := range node.ObjectMeta.Labels {
			fullLabel := key + "=" + value

			if _, exists := nodeGrp2nodes[fullLabel]; !exists {
				nodeGrp2nodes[fullLabel] = sets.NewString()
			}
			nodeGrp2nodes[fullLabel].Insert(string(node.UID))

			if _, exists := nodeGrp2workloads[fullLabel]; !exists {
				nodeGrp2workloads[fullLabel] = sets.NewString()
			}
			nodeGrp2workloads[fullLabel].Insert(allWorkloadsOnNode.List()...)
		}
	}
	// start building the NodeGroup entity dto
	for fullLabel, nodeLst := range nodeGrp2nodes {
		labelparts := strings.Split(fullLabel, "=")
		// build nodegroup entities only if there is a pod which has a rule that uses that
		// particular topology key. This already does not include hostname as the topology key
		if len(labelparts) > 0 && !builder.otherSpreadTopologyKeys.Has(labelparts[0]) {
			continue
		}
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NODE_GROUP, fullLabel+"@"+builder.clusterSummary.Name)
		entityDTOBuilder.IsProvisionable(false)
		entityDTOBuilder.IsSuspendable(false)

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

			// Build node2nodeGrp
			if _, exists := node2nodeGrp[nodeUID]; !exists {
				node2nodeGrp[nodeUID] = sets.NewString()
			}
			node2nodeGrp[nodeUID].Insert(entityDto.GetId())
		}

		// Fill in commodities
		soldCommodities, _ := builder.getCommoditiesSold(fullLabel, nodeGrp2workloads[fullLabel])
		if len(soldCommodities) > 0 {
			entityDto.CommoditiesSold = soldCommodities
		}

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}
	return result, node2nodeGrp
}

func (builder *nodeGroupEntityDTOBuilder) getCommoditiesSold(fullLabel string, workloadLst sets.String) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	//Anti-Affinity: segmentation commodity
	for tpkey, workloads := range builder.otherSpreadWorkloads {
		labelparts := strings.Split(fullLabel, "=")
		if len(labelparts) > 0 && labelparts[0] == tpkey {
			for wlname, _ := range workloads {
				glog.V(4).Infof("Add segmentation commodity with the key %v for the node_group entity %v", wlname, fullLabel)
				used := 0.0
				if workloadLst.Has(wlname) {
					used = 1.0
				}

				commodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
					Key(tpkey + "@" + wlname).
					Capacity(1).
					Used(used).
					Create()

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
