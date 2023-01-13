package compliance

import (
	"fmt"
	"testing"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/turbonomic/kubeturbo/pkg/discovery/cache"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/parallelizer"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func TestProcessAffinityPerPod(t *testing.T) {
	nodes, pods, pvs := generateTopology()
	table := []struct {
		nodes     []*api.Node
		pods      []*api.Pod
		pod2PvMap map[string][]repository.MountedVolume

		// process affinity rules on the given pod.
		currPod  *api.Pod
		currNode *api.Node
		// a slice of expected matching node.
		expectedMatchingNodes []*api.Node
		// a slice of expected matching node.
		expectedNonMatchingNodes []*api.Node
		// the expected number of affinity access commodities bought by the currPod.
		expectedAffinityCommoditiesNumber int

		text string
	}{
		{
			nodes: nodes,
			pods:  pods,

			currPod:  pods[0],
			currNode: nodes[0],
			expectedMatchingNodes: []*api.Node{
				nodes[0],
				nodes[2],
			},
			expectedNonMatchingNodes: []*api.Node{
				nodes[1],
			},
			expectedAffinityCommoditiesNumber: 1,

			text: "test node matches node affinity rules.",
		},
		{
			nodes: nodes,
			pods:  pods,

			currPod:  pods[1],
			currNode: nodes[0],
			expectedMatchingNodes: []*api.Node{
				nodes[0],
			},
			expectedNonMatchingNodes: []*api.Node{
				nodes[1],
				nodes[2],
			},
			expectedAffinityCommoditiesNumber: 1,

			text: "test node matches pod affinity rules by hosting the pod matches pod affinity terms.",
		},
		{
			nodes: nodes,
			pods:  pods,

			currPod:  pods[2],
			currNode: nodes[0],
			expectedMatchingNodes: []*api.Node{
				nodes[0],
				nodes[2],
			},
			expectedNonMatchingNodes: []*api.Node{
				nodes[1],
			},
			expectedAffinityCommoditiesNumber: 1,

			text: "test node matches pod affinity rules by having the same topology key.",
		},
		{
			nodes: nodes,
			pods:  pods,
			pod2PvMap: map[string][]repository.MountedVolume{
				util.GetPodClusterID(pods[3]): {
					{
						MountName:  "mount-point-name",
						UsedVolume: pvs[0],
					},
				},
			},

			currPod:  pods[3],
			currNode: nodes[1],
			expectedMatchingNodes: []*api.Node{
				nodes[1],
			},
			expectedNonMatchingNodes: []*api.Node{
				nodes[0],
				nodes[2],
			},
			expectedAffinityCommoditiesNumber: 1,

			text: "test node matches PV affinity rules.",
		},
	}

	for i, item := range table {
		entityDTOs := generateAllBasicEntityDTOs(item.nodes, item.pods)
		ap := newAffinityProcessorForTest(item.nodes, item.pods, item.pod2PvMap)
		ap.GroupEntityDTOs(entityDTOs)

		ap.processAffinityPerPod(item.currPod)

		podEntityDTO, err := ap.GetEntityDTO(proto.EntityDTO_CONTAINER_POD, string(item.currPod.UID))
		if err != nil {
			t.Errorf("Test case %d failed. Cannot find pod entityDTO after calling function. Err is %s", i, err)
			continue
		}
		commBoughtFromCurrProvider := getCommoditiesBoughtFromGivenProvider(podEntityDTO, string(item.currNode.UID))
		accessCommBought := getCommoditiesOfGivenType(commBoughtFromCurrProvider, proto.CommodityDTO_VMPM_ACCESS)
		// check if the number of accessCommodities bought are as expected.
		if item.expectedAffinityCommoditiesNumber != len(accessCommBought) {
			t.Errorf("Test case %d failed. Number of affinity commodities bought is not correct. Exptected %d, got %d", i, item.expectedAffinityCommoditiesNumber, len(accessCommBought))
			continue
		}
		// check if commodities sold and bought match
		providerNodeEntityDTOs := []*proto.EntityDTO{}
		for _, nn := range item.expectedMatchingNodes {
			nodeEntityDTO, err := ap.GetEntityDTO(proto.EntityDTO_VIRTUAL_MACHINE, string(nn.UID))
			if err != nil {
				t.Errorf("Test case %d failed. Cannot find node entityDTO after calling function. Err is %s", i, err)
				continue
			}
			providerNodeEntityDTOs = append(providerNodeEntityDTOs, nodeEntityDTO)
		}
		if len(providerNodeEntityDTOs) != len(item.expectedMatchingNodes) {
			continue
		}
		for _, nodeEntityDTO := range providerNodeEntityDTOs {
			if pass, err := checkAccessCommodityBuyerSellerRelationship(podEntityDTO, nodeEntityDTO, accessCommBought); !pass {
				t.Errorf("Consumer provider commodity doesn't match: %s", err)
			}
		}

		// check if commodities are added to those nodes don't match affinity rules.
		nonProviderNodeEntityDTOs := []*proto.EntityDTO{}
		for _, nn := range item.expectedNonMatchingNodes {
			nodeEntityDTO, err := ap.GetEntityDTO(proto.EntityDTO_VIRTUAL_MACHINE, string(nn.UID))
			if err != nil {
				t.Errorf("Test case %d failed. Cannot find node entityDTO after calling function. Err is %s", i, err)
				continue
			}
			nonProviderNodeEntityDTOs = append(nonProviderNodeEntityDTOs, nodeEntityDTO)
		}
		if len(nonProviderNodeEntityDTOs) != len(item.expectedNonMatchingNodes) {
			continue
		}
		for _, nodeEntityDTO := range nonProviderNodeEntityDTOs {
			if pass, _ := checkAccessCommodityBuyerSellerRelationship(podEntityDTO, nodeEntityDTO, accessCommBought); pass {
				t.Errorf("Test case %d failed. Unexpected provider %s for pod %s", i, nodeEntityDTO.GetId(), podEntityDTO.GetId())
			}
		}
	}
}

// Check if a seller sells all the accessCommodities of the given type.
// TODO as here we use VMPMAccessCommodity to model affinity/anti-affinity rules, there is no way to differentiate them from VMPMAccessCommodities created by other constraints.
func checkAccessCommodityBuyerSellerRelationship(consumer, seller *proto.EntityDTO, accessCommoditiesBought []*proto.CommodityDTO) (bool, error) {
	commoditiesSold := seller.GetCommoditiesSold()
	accessCommoditiesSold := getCommoditiesOfGivenType(commoditiesSold, proto.CommodityDTO_VMPM_ACCESS)
	soldMap := getAccessCommoditiesMap(accessCommoditiesSold)

	for _, commBought := range accessCommoditiesBought {
		if _, found := soldMap[commBought.GetKey()]; !found {
			return false, fmt.Errorf("Expected commodity with key %s bought by %s doesn't find in "+
				"commodities sold by %s", commBought.GetKey(), consumer.GetId(), seller.GetId())
		}
	}
	return true, nil
}

func getCommoditiesOfGivenType(commodities []*proto.CommodityDTO, cType proto.CommodityDTO_CommodityType) []*proto.CommodityDTO {
	comms := []*proto.CommodityDTO{}
	for _, comm := range commodities {
		if comm.GetCommodityType() == cType {
			comms = append(comms, comm)
		}
	}
	return comms
}

func getCommoditiesBoughtFromGivenProvider(consumer *proto.EntityDTO, providerID string) []*proto.CommodityDTO {
	boughtCommodityTypes := consumer.GetCommoditiesBought()
	for _, bc := range boughtCommodityTypes {
		if bc.GetProviderId() == providerID { // TODO bc.GetProviderType() compare
			return bc.GetBought()
		}
	}
	return []*proto.CommodityDTO{}
}

// key: commodity key; value: commodity
func getAccessCommoditiesMap(accessCommodities []*proto.CommodityDTO) map[string]*proto.CommodityDTO {
	mm := make(map[string]*proto.CommodityDTO)
	for _, comm := range accessCommodities {
		commType := comm.GetCommodityType()
		if commType == proto.CommodityDTO_VMPM_ACCESS {
			mm[comm.GetKey()] = comm
		}
	}
	return mm
}

func generateTopology() ([]*api.Node, []*api.Pod, []*api.PersistentVolume) {
	nodeName1 := "node1"
	nodeLabel1 := map[string]string{
		"zone":   "zone_value1",
		"region": "region_value1",
	}

	nodeName2 := "node2"
	nodeLabel2 := map[string]string{
		"zone": "zone_value2",
	}

	nodeName3 := "node3"
	nodeLabel3 := map[string]string{
		"zone": "zone_value1",
	}

	allNodes := []*api.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName1,
				UID:    types.UID(nodeName1),
				Labels: nodeLabel1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName2,
				UID:    types.UID(nodeName2),
				Labels: nodeLabel2,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName3,
				UID:    types.UID(nodeName3),
				Labels: nodeLabel3,
			},
		},
	}

	podName1 := "pod1"
	podName2 := "pod2"
	podName3 := "pod3"
	podName4 := "pod4"

	allPods := []*api.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   podName1,
				UID:    types.UID(podName1),
				Labels: map[string]string{"service": "securityscan"},
			},
			Spec: api.PodSpec{
				Affinity: &api.Affinity{
					NodeAffinity: &api.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
							NodeSelectorTerms: []api.NodeSelectorTerm{
								{
									MatchExpressions: []api.NodeSelectorRequirement{
										{
											Key:      "zone",
											Operator: api.NodeSelectorOpIn,
											Values:   []string{"zone_value1"},
										},
									},
								},
							},
						},
					},
				},
				NodeName: nodeName1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: podName2,
				UID:  types.UID(podName2),
			},
			Spec: api.PodSpec{
				Affinity: &api.Affinity{
					PodAffinity: &api.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "service",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"securityscan", "value2"},
										},
									},
								},
								TopologyKey: "region",
							},
						},
					},
				},
				NodeName: nodeName1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: podName3,
				UID:  types.UID(podName3),
			},
			Spec: api.PodSpec{
				Affinity: &api.Affinity{
					PodAffinity: &api.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "service",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"securityscan"},
										},
									},
								},
								TopologyKey: "zone",
							},
						},
					},
				},
				NodeName: nodeName1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: podName4,
				UID:  types.UID(podName4),
			},
			Spec: api.PodSpec{
				NodeName: nodeName2,
			},
		},
	}

	pvName1 := "pv1"
	allPVs := []*api.PersistentVolume{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: pvName1,
				UID:  types.UID(pvName1),
			},
			Spec: api.PersistentVolumeSpec{
				NodeAffinity: &api.VolumeNodeAffinity{
					Required: &api.NodeSelector{
						NodeSelectorTerms: []api.NodeSelectorTerm{
							{
								MatchExpressions: []api.NodeSelectorRequirement{
									{
										Key:      "zone",
										Operator: api.NodeSelectorOpIn,
										Values:   []string{"zone_value2"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return allNodes, allPods, allPVs
}

func generateAllBasicEntityDTOs(allNodes []*api.Node, allPods []*api.Pod) []*proto.EntityDTO {
	var allEntityDTOs []*proto.EntityDTO
	for _, node := range allNodes {
		allEntityDTOs = append(allEntityDTOs, &proto.EntityDTO{
			EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
			Id:         getStringPointer(string(node.UID)),
		})
	}
	for _, pod := range allPods {
		allEntityDTOs = append(allEntityDTOs, &proto.EntityDTO{
			EntityType: getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
			Id:         getStringPointer(string(pod.UID)),
		})
	}

	return allEntityDTOs
}

func newAffinityProcessorForTest(allNodes []*api.Node, allPods []*api.Pod, pod2PVs map[string][]repository.MountedVolume) *AffinityProcessor {
	return &AffinityProcessor{
		ComplianceProcessor: NewComplianceProcessor(),
		commManager:         NewAffinityCommodityManager(),

		nodes:                 allNodes,
		pods:                  allPods,
		podToVolumesMap:       pod2PVs,
		parallelizer:          parallelizer.NewParallelizer(parallelizer.DefaultParallelism),
		affinityPodNodesCache: cache.NewAffinityPodNodesCache(allNodes, allPods),
	}
}
