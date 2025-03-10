package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apiserver/pkg/util/feature"
)

func TestNodeGroupEntityCreation(t *testing.T) {
	otherSpreadTopologyKeys := sets.NewString("zone", "region")
	table := []struct {
		nodes             []*api.Node
		expectedEntityNum int
	}{
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":   "zone1",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region1",
						},
					},
				},
			},
			expectedEntityNum: 3,
		},
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":   "zone1",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region2",
						},
					},
				},
			},
			expectedEntityNum: 4,
		},
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":                   "zone1",
							"region":                 "region1",
							"kubernetes.io/hostname": "node1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":                   "zone2",
							"region":                 "region1",
							"kubernetes.io/hostname": "node2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"zone":                   "zone2",
							"region":                 "region2",
							"kubernetes.io/hostname": "node3",
						},
					},
				},
			},
			expectedEntityNum: 4,
		},
	}

	for _, item := range table {
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		affinityMapper := podaffinity.NewAffinityMapper(nil, nil)
		nodeGroupEntityDTOBuilder := NewNodeGroupEntityDTOBuilder(clusterSummary, nil, otherSpreadTopologyKeys, affinityMapper)
		nodeGroupDTOs, _ := nodeGroupEntityDTOBuilder.BuildEntityDTOs()
		assert.Equal(t, item.expectedEntityNum, len(nodeGroupDTOs))
	}
}

func Test_BuildNodegroup_with_p2p_Affinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=true")
	type Expectation struct {
		commodityBoughtKey string
		used               float64
	}

	table := []struct {
		topologyKey       string
		nodes             []*api.Node
		pods              []*api.Pod
		node2NodeGrpCount int
		expectations      map[string]Expectation
	}{
		{
			topologyKey: "zone",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						UID:  "node0",
						Labels: map[string]string{
							"zone": "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						UID:  "node1",
						Labels: map[string]string{
							"zone": "zone1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
					},
					Spec: api.PodSpec{
						Affinity: &api.Affinity{
							PodAffinity: &api.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "app",
													Operator: metav1.LabelSelectorOpIn,
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "zone",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			node2NodeGrpCount: 2,
			expectations: map[string]Expectation{
				"zone=zone0@MyCluster": {
					commodityBoughtKey: "zone|testns/webserver|testns/db",
					used:               podaffinity.NONE,
				},
				"zone=zone1@MyCluster": {
					commodityBoughtKey: "zone|testns/webserver|testns/db",
					used:               1,
				},
			},
		},
		{
			topologyKey: "kubernetes.io/hostname",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						UID:  "node0",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						UID:  "node1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
					},
					Spec: api.PodSpec{
						Affinity: &api.Affinity{
							PodAffinity: &api.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "app",
													Operator: metav1.LabelSelectorOpIn,
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			node2NodeGrpCount: 0,
			expectations: map[string]Expectation{
				"kubernetes.io/hostname=node0@MyCluster": {
					commodityBoughtKey: "kubernetes.io/hostname|testns/webserver|testns/db",
					used:               podaffinity.NONE,
				},
				"kubernetes.io/hostname=node1@MyCluster": {
					commodityBoughtKey: "kubernetes.io/hostname|testns/webserver|testns/db",
					used:               podaffinity.NONE,
				},
			},
		},
	}

	for _, item := range table {
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		nodeInfoListener := podaffinity.NewNodeInfoLister(clusterSummary)

		srcPodInfo, _ := podaffinity.NewPodInfo(item.pods[0])
		dstPodInfo, _ := podaffinity.NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]

		affinityMapper := podaffinity.NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoListener)
		affinityMapper.BuildAffinityMaps(srcPodInfo.RequiredAffinityTerms, srcPodInfo, dstPodInfo, dstNode, podaffinity.Affinity)

		assert.True(t, affinityMapper != nil)

		otherSpreadTopologyKeys := sets.NewString()
		nodeGroupEntityDTOBuilder := NewNodeGroupEntityDTOBuilder(clusterSummary, nil, otherSpreadTopologyKeys, affinityMapper)
		nodeGroupDTOs, node2nodegroup := nodeGroupEntityDTOBuilder.BuildEntityDTOs()

		for _, nodeGroupDTO := range nodeGroupDTOs {
			expectation, exists := item.expectations[*nodeGroupDTO.Id]
			assert.True(t, exists)

			assert.True(t, len(nodeGroupDTO.CommoditiesSold) == 1)
			targetBought := nodeGroupDTO.CommoditiesSold[0]
			assert.True(t, *targetBought.Key == expectation.commodityBoughtKey)
			assert.True(t, *targetBought.Used == expectation.used)
			assert.True(t, *targetBought.CommodityType == proto.CommodityDTO_PEER_TO_PEER_AFFINITY)
		}
		assert.True(t, len(node2nodegroup) == item.node2NodeGrpCount)
	}
}
