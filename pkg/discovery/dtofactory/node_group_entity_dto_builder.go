package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type nodeGroupEntityDTOBuilder struct {
	clusterSummary           *repository.ClusterSummary
	otherSpreadWorkloads     map[string]sets.String
	topologyKey2affinityTerm map[string][]podaffinity.AffinityTerm
}

const (
	affinityCommodityDefaultCapacity = 1e10
)

func NewNodeGroupEntityDTOBuilder(clusterSummary *repository.ClusterSummary, otherSpreadWorkloads map[string]sets.String, topologyKey2affinityTerm map[string][]podaffinity.AffinityTerm) *nodeGroupEntityDTOBuilder {
	return &nodeGroupEntityDTOBuilder{
		clusterSummary:           clusterSummary,
		otherSpreadWorkloads:     otherSpreadWorkloads,
		topologyKey2affinityTerm: topologyKey2affinityTerm,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeGroupEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	nodeGrp2nodes := make(map[string]sets.String) // Map of nodeGroup ---> nodes
	nodeGrp2pods := make(map[string][]*api.Pod)
	for _, node := range builder.clusterSummary.Nodes {
		for key, value := range node.ObjectMeta.Labels {
			fullLabel := key + "=" + value
			nodeLst := nodeGrp2nodes[fullLabel]
			if nodeLst.Len() == 0 {
				nodeLst = sets.NewString(node.Name)
			} else {
				nodeLst.Insert(node.Name)
			}
			nodeGrp2nodes[fullLabel] = nodeLst
			podsOnNodeGrp := append(nodeGrp2pods[fullLabel], builder.clusterSummary.NodeToRunningPods[node.Name]...)
			nodeGrp2pods[fullLabel] = podsOnNodeGrp
		}
	}
	// start building the NodeGroup entity dto
	for fullLabel, nodeLst := range nodeGrp2nodes {
		if strings.HasPrefix(fullLabel, "kubernetes.io/hostname") {
			continue //no need to create entity for hostname label
		}
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NODE_GROUP, fullLabel)
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build NodeGroup entityDTO: %s", err)
			continue
		}

		// Filling in ConnectedEntity field to reflect the mapping between the nodegroup and node
		for nodeName := range nodeLst {
			connectedEntityID := nodeName
			connectedEntityType := proto.ConnectedEntity_NORMAL_CONNECTION
			entityDto.ConnectedEntities = append(entityDto.ConnectedEntities, &proto.ConnectedEntity{
				ConnectedEntityId: &connectedEntityID,
				ConnectionType:    &connectedEntityType,
			})
		}

		// Fill in commodities
		soldCommodities, _ := builder.getCommoditiesSold(fullLabel, nodeGrp2pods)
		entityDto.CommoditiesSold = soldCommodities

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}
	return result, nil
}

func (builder *nodeGroupEntityDTOBuilder) getCommoditiesSold(fullLabel string, nodeGrp2pods map[string][]*api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO

	//Affinity: peer_to_peer commodity
	for tpkey, affinityTerms := range builder.topologyKey2affinityTerm {
		if strings.HasPrefix(fullLabel, tpkey) {
			allExistingPods := nodeGrp2pods[fullLabel]
			for _, aterm := range affinityTerms {
				match := false
				for _, apod := range allExistingPods {
					if aterm.Matches(apod, nil) {
						match = true
						break
					}
				}
				if match {
					commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY)
					commSoldBuilder.Key(aterm.Selector.String() + "@" + tpkey)
					commSoldBuilder.Capacity(affinityCommodityDefaultCapacity)
					commodity, err := commSoldBuilder.Create()
					if err != nil {
						glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_AFFINITY, err)
						continue
					}
					commoditiesSold = append(commoditiesSold, commodity)
				}
			}
		}
	}

	//Anti-Affinity: segmentation commodity
	for tpkey, workloads := range builder.otherSpreadWorkloads {
		if strings.HasPrefix(fullLabel, tpkey) {
			for wlname, _ := range workloads {
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
