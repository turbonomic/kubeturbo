package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
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
	nodeGrp2nodes := make(map[string]sets.String)     // Map of nodeGroup ---> nodes
	nodeGrp2workloads := make(map[string]sets.String) // Map of nodeGroup ---> workloads of all pods on each node of the nodegroup
	for _, node := range builder.clusterSummary.Nodes {
		allPods := append(builder.clusterSummary.GetPendingPodsOnNode(node), builder.clusterSummary.GetRunningPodsOnNode(node)...)
		for key, value := range node.ObjectMeta.Labels {
			fullLabel := key + "=" + value

			if _, exists := nodeGrp2nodes[fullLabel]; !exists {
				nodeGrp2nodes[fullLabel] = sets.NewString()
			}
			nodeGrp2nodes[fullLabel].Insert(string(node.UID))

			//add code for nodeGrp2workload
			if _, exists := nodeGrp2workloads[fullLabel]; !exists {
				nodeGrp2workloads[fullLabel] = sets.NewString()
			}

			for _, pod := range allPods {
				podControllerInfoKey := util.PodControllerInfoKey(pod)
				nodeGrp2nodes[fullLabel].Insert(podControllerInfoKey)
			}
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
		soldCommodities, _ := builder.getCommoditiesSold(fullLabel, nodeGrp2workloads)
		if len(soldCommodities) > 0 {
			entityDto.CommoditiesSold = soldCommodities
		}

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}
	return result, nil
}

func (builder *nodeGroupEntityDTOBuilder) getCommoditiesSold(fullLabel string, workloadLst map[string]sets.String) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	//Anti-Affinity: segmentation commodity
	for tpkey, workloads := range builder.otherSpreadWorkloads {
		labelparts := strings.Split(fullLabel, "=")
		if len(labelparts) > 0 && labelparts[0] == tpkey {
			for wlname, _ := range workloads {
				glog.V(4).Infof("Add segmentation commodity with the key %v for the node_group entity %v", wlname, fullLabel)
				used := 0.0
				if workloads.Has(wlname) {
					used = 1.0
				}

				commodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
					Key(wlname).
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
