package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

type nodeGroupEntityDTOBuilder struct {
	clusterSummary          *repository.ClusterSummary
	otherSpreadWorkloads    map[string]sets.String
	otherSpreadTopologyKeys sets.String
	affinityMapper          *podaffinity.AffinityMapper
}

const (
	affinityCommodityDefaultCapacity = 1e10
)

func NewNodeGroupEntityDTOBuilder(clusterSummary *repository.ClusterSummary, otherSpreadWorkloads map[string]sets.String,
	otherSpreadTopologyKeys sets.String, affinityMapper *podaffinity.AffinityMapper) *nodeGroupEntityDTOBuilder {
	return &nodeGroupEntityDTOBuilder{
		clusterSummary:          clusterSummary,
		otherSpreadWorkloads:    otherSpreadWorkloads,
		otherSpreadTopologyKeys: otherSpreadTopologyKeys,
		affinityMapper:          affinityMapper,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeGroupEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, map[string]sets.String) {
	var result []*proto.EntityDTO
	node2nodeGrp := make(map[string]sets.String)   // Map of node ---> NodeGroup
	nodeGrp2nodes := make(map[string]sets.String)  // Map of nodeGroup ---> nodes
	node2workloads := make(map[string]sets.String) // Map of node UID ---> workloads of all pods on each node of the nodegroup
	nodeGrp2pods := make(map[string]sets.String)   // Map of nodeGroup ---> all pods on nodes within that nodegroup

	for _, node := range builder.clusterSummary.Nodes {
		allWorkloadsOnNode := getAllWorkloadsOnNode(node, builder.clusterSummary)
		allPodsOnNode := getAllPodsOnNode(node, builder.clusterSummary)
		for key, value := range node.ObjectMeta.Labels {
			if key == "kubernetes.io/hostname" {
				continue
			}
			fullLabel := key + "=" + value

			if _, exists := nodeGrp2nodes[fullLabel]; !exists {
				nodeGrp2nodes[fullLabel] = sets.NewString()
			}
			nodeGrp2nodes[fullLabel].Insert(string(node.UID))

			if _, exists := nodeGrp2pods[fullLabel]; !exists {
				nodeGrp2pods[fullLabel] = sets.NewString()
			}
			nodeGrp2pods[fullLabel].Insert(allPodsOnNode.UnsortedList()...)

			if _, exists := node2workloads[string(node.UID)]; !exists {
				node2workloads[string(node.UID)] = sets.NewString()
			}
			node2workloads[string(node.UID)].Insert(allWorkloadsOnNode.List()...)
		}
	}

	// start building the NodeGroup entity dto
	for fullLabel, nodeLst := range nodeGrp2nodes {
		labelparts := strings.Split(fullLabel, "=")
		// Prior to PeerToPeerAffinityAntiaffinity=true, we would build nodegroups only if there are
		// split (antiaffinity to self) workloads based on topology!=hostname and only to service those
		// workloads. This was based on "otherSpreadopologyKeys" (if it contains the topologykey).
		//
		// With PeerToPeerAffinityAntiaffinity=true we would create the nodegroups for the above and
		// also for all other topology keys (!=hostname) which feature in other affinity/antiaffinity rules
		// We base the additional nodegroups on affinityMapper.GetNodeGroupKeys() being available for that topology
		peerToPeerAffinityAntiaffinity := utilfeature.DefaultFeatureGate.Enabled(features.PeerToPeerAffinityAntiaffinity)
		segmentationBasedTopologySpread := utilfeature.DefaultFeatureGate.Enabled(features.SegmentationBasedTopologySpread)
		var exists bool
		switch {
		case !(len(labelparts) > 0):
			continue
		case segmentationBasedTopologySpread && !peerToPeerAffinityAntiaffinity && !builder.otherSpreadTopologyKeys.Has(labelparts[0]):
			// Only segmentation based spread enabled we rely on otherSpreadTopologyKeys to create nodegroups
			continue
		case peerToPeerAffinityAntiaffinity:
			// we rely on affinityMapper.GetNodeGroupKeys() to build the nodegroups
			// this should also ensures that we do still consider otherSpreadTopologyKeys (in case
			//	segmentationBasedTopologySpread is also enabled)
			_, exists = builder.affinityMapper.GetNodeGroupKeys(labelparts[0])
			if !exists && !builder.otherSpreadTopologyKeys.Has(labelparts[0]) {
				continue
			}

		}

		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NODE_GROUP, fullLabel+"@"+builder.clusterSummary.Name)
		entityDTOBuilder.IsProvisionable(true)
		entityDTOBuilder.IsSuspendable(false)

		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build NodeGroup entityDTO: %s", err)
			continue
		}

		nodeGroupWorkloads := make(map[string]int)

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

			// this is needed to get pod counts against workloads in this topology for segmentation commodities
			nodeGroupWorkloads = getWorkloadCounts(nodeGrp2pods[fullLabel], builder.clusterSummary.PodToControllerMap)
		}

		// Fill in commodities
		soldCommodities, _ := builder.getCommoditiesSold(fullLabel, nodeGroupWorkloads)
		if len(soldCommodities) > 0 {
			entityDto.CommoditiesSold = soldCommodities
		}

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}

	return result, node2nodeGrp
}

func getWorkloadCounts(pods sets.String, podtoControllers map[string]string) map[string]int {
	wCounts := make(map[string]int)
	for _, pod := range pods.UnsortedList() {
		if w, found := podtoControllers[pod]; found {
			wCounts[w] += 1
		}
	}
	return wCounts
}

func (builder *nodeGroupEntityDTOBuilder) getCommoditiesSold(fullLabel string, workloadCounts map[string]int) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO

	if utilfeature.DefaultFeatureGate.Enabled(features.SegmentationBasedTopologySpread) {
		// Nodegroups created from a given topology, eg all zone based ones will sell
		// segmentation commodities for workloads listed for that topology key
		labelparts := strings.Split(fullLabel, "=")
		tpkey := ""
		if len(labelparts) > 0 {
			tpkey = labelparts[0]
		}

		for wlname := range builder.otherSpreadWorkloads[tpkey] {
			used := 0.0
			if count, found := workloadCounts[wlname]; found {
				// if this nodegoup eg zone=z1 has this workload pods then used value
				// will be set else used will remain 0.0
				used = float64(count)
			}
			key := tpkey + "@" + wlname
			glog.V(4).Infof("Add segmentation commodity with the key %v for the node_group entity %v", key, fullLabel)
			commodity, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
				Key(key).
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

	if utilfeature.DefaultFeatureGate.Enabled(features.PeerToPeerAffinityAntiaffinity) {
		soldAffinityKeys := sets.Set[string]{}
		soldAntiAffinityKeys := sets.Set[string]{}
		providerMapping, providerMappingExists := builder.affinityMapper.GetProviderMapping(fullLabel)
		if providerMappingExists {
			for key := range providerMapping.Keys.AffinityKeys {
				used := 0.0
				if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AffinitySrc}]; exists {
					used += float64(podList.Len()) * podaffinity.NONE
				}
				if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AffinityDst}]; exists {
					used += float64(podList.Len())
				}
				affinityComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY).
					Key(key).
					Used(used).
					Capacity(affinityCommodityDefaultCapacity).
					Create()

				if err != nil {
					glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_AFFINITY, err)
					continue
				}
				commoditiesSold = append(commoditiesSold, affinityComm)

				// Store the commodity key to ensure it gets skipped when processing remaining node group keys below
				soldAffinityKeys.Insert(key)
			}

			for key := range providerMapping.Keys.AntiAffinityKeys {
				used := 0.0
				peak := 0.0
				if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AntiAffinitySrc}]; exists {
					used += float64(podList.Len())
					peak += float64(podList.Len()) * podaffinity.NONE
				}
				if podList, exists := providerMapping.KeyCounts[podaffinity.MappingKey{CommodityKey: key, MappingType: podaffinity.AntiAffinityDst}]; exists {
					used += float64(podList.Len()) * podaffinity.NONE
					peak += float64(podList.Len())
				}
				antiAffinityComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY).
					Key(key).
					Used(float64(used)).
					Peak(float64(peak)).
					Capacity(affinityCommodityDefaultCapacity).
					Create()

				if err != nil {
					glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY, err)
					continue
				}
				commoditiesSold = append(commoditiesSold, antiAffinityComm)

				// Store the commodity key to ensure it gets skipped when processing remaining node group keys below
				soldAntiAffinityKeys.Insert(key)
			}
		}

		// It is possible that this node group is not currently a provider for affinity/anti-affinity workloads, but could be in
		// the future. Therefore, this node group needs to sell the same keys as the actual providers do.
		// Provider mappings will include the topologyKey and value (i.e. "topologyKey=value"). To find the remaining keys this
		// node group should sell, we need to drop the value, so that just the topologyKey remains.
		parts := strings.Split(fullLabel, "=")
		if len(parts) > 0 {
			topologyKey := parts[0]
			if mappingKeys, exists := builder.affinityMapper.GetNodeGroupKeys(topologyKey); exists {
				for mappingKey := range mappingKeys {
					switch mappingKey.MappingType {
					case podaffinity.AffinitySrc, podaffinity.AffinityDst:
						if !soldAffinityKeys.Has(mappingKey.CommodityKey) {
							// A sold commodity has not been generated for this key
							affinityComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY).
								Key(mappingKey.CommodityKey).
								Used(0).
								Capacity(affinityCommodityDefaultCapacity).
								Create()

							if err != nil {
								glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_AFFINITY, err)
								continue
							}
							commoditiesSold = append(commoditiesSold, affinityComm)
						}
					case podaffinity.AntiAffinitySrc, podaffinity.AntiAffinityDst:
						if !soldAntiAffinityKeys.Has(mappingKey.CommodityKey) {
							// A sold commodity has not been generated for this key
							antiAffinityComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY).
								Key(mappingKey.CommodityKey).
								Used(0).
								Peak(0).
								Capacity(affinityCommodityDefaultCapacity).
								Create()

							if err != nil {
								glog.Errorf("Failed to build commodity sold %s: %v", proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY, err)
								continue
							}
							commoditiesSold = append(commoditiesSold, antiAffinityComm)
						}
					}

				}
			}
		}
	}

	return commoditiesSold, nil

}
