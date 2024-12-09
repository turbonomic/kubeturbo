package podaffinity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ZoneAsTopologyKey     = "zone"
	HostNameAsTopologyKey = "kubernetes.io/hostname"
)

func TestAffinintyMapper(t *testing.T) {
	table := []struct {
		topologyKey     string
		nodes           []*api.Node
		pods            []*api.Pod
		expectExistence bool
	}{
		{
			topologyKey: ZoneAsTopologyKey,
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							ZoneAsTopologyKey: "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							ZoneAsTopologyKey: "zone1",
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
										TopologyKey: ZoneAsTopologyKey,
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
			expectExistence: true,
		},
		{
			topologyKey: HostNameAsTopologyKey,
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							HostNameAsTopologyKey: "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							HostNameAsTopologyKey: "node1",
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
										TopologyKey: HostNameAsTopologyKey,
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
			expectExistence: true,
		},
	}

	for _, item := range table {
		clusterSummary := createTestClusterSummary(item.pods, item.nodes)
		nodeInfoLister := NewNodeInfoLister(clusterSummary)
		am := NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoLister)
		srcPodInfo, _ := NewPodInfo(item.pods[0])
		srcNode := item.nodes[0]
		dstPodInfo, _ := NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]
		var srcProviderId, dstProviderId string
		var providerType ProviderType
		if item.topologyKey == HostNameAsTopologyKey {
			srcProviderId = srcNode.Name
			dstProviderId = dstNode.Name
			providerType = NodeProvider
		} else {
			srcProviderId = item.topologyKey + "=" + srcNode.Labels[item.topologyKey]
			dstProviderId = item.topologyKey + "=" + dstNode.Labels[item.topologyKey]
			providerType = NodeGroupProvider
		}
		am.BuildAffinityMaps(srcPodInfo.RequiredAffinityTerms, srcPodInfo, dstPodInfo, dstNode, Affinity)

		// Check the podMappings
		allSrcMappings := am.podMappings[util.PodKeyFunc(srcPodInfo.Pod)]
		allDstMappings := am.podMappings[util.PodKeyFunc(dstPodInfo.Pod)]
		srcMapping := PodMapping{
			MappingKey: MappingKey{
				CommodityKey: item.topologyKey + "|" + util.PodKeyFunc(srcPodInfo.Pod) + "|" + util.PodKeyFunc(dstPodInfo.Pod),
				MappingType:  AffinitySrc,
			},
			Provider: Provider{
				ProviderType: providerType,
				ProviderId:   srcProviderId,
			},
		}
		_, exists1 := allSrcMappings[srcMapping]
		dstMapping := PodMapping{
			MappingKey: MappingKey{
				CommodityKey: item.topologyKey + "|" + util.PodKeyFunc(srcPodInfo.Pod) + "|" + util.PodKeyFunc(dstPodInfo.Pod),
				MappingType:  AffinityDst,
			},
			Provider: Provider{
				ProviderType: providerType,
				ProviderId:   dstProviderId,
			},
		}
		_, exists2 := allDstMappings[dstMapping]
		assert.True(t, (exists1 && exists2) == item.expectExistence)

		// Check the ProviderMappings
		srcProviderMapping := am.providerMappings[srcProviderId]
		dstProviderMapping := am.providerMappings[dstProviderId]
		assert.True(t, srcProviderMapping.Keys.AffinityKeys.Has(item.topologyKey+"|"+util.PodKeyFunc(srcPodInfo.Pod)+"|"+util.PodKeyFunc(dstPodInfo.Pod)))
		assert.True(t, dstProviderMapping.Keys.AffinityKeys.Has(item.topologyKey+"|"+util.PodKeyFunc(srcPodInfo.Pod)+"|"+util.PodKeyFunc(dstPodInfo.Pod)))

	}

}

func TestAntiAffinintyMapper(t *testing.T) {
	table := []struct {
		topologyKey     string
		nodes           []*api.Node
		pods            []*api.Pod
		expectExistence bool
	}{
		{
			topologyKey: ZoneAsTopologyKey,
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							ZoneAsTopologyKey: "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							ZoneAsTopologyKey: "zone1",
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
							PodAntiAffinity: &api.PodAntiAffinity{
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
										TopologyKey: ZoneAsTopologyKey,
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
			expectExistence: true,
		},
		{
			topologyKey: HostNameAsTopologyKey,
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							HostNameAsTopologyKey: "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							HostNameAsTopologyKey: "node1",
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
							PodAntiAffinity: &api.PodAntiAffinity{
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
										TopologyKey: HostNameAsTopologyKey,
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
			expectExistence: true,
		},
	}

	for _, item := range table {
		clusterSummary := createTestClusterSummary(item.pods, item.nodes)
		nodeInfoLister := NewNodeInfoLister(clusterSummary)
		am := NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoLister)
		srcPodInfo, _ := NewPodInfo(item.pods[0])
		srcNode := item.nodes[0]
		dstPodInfo, _ := NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]
		var srcProviderId, dstProviderId string
		var providerType ProviderType
		if item.topologyKey == HostNameAsTopologyKey {
			srcProviderId = srcNode.Name
			dstProviderId = dstNode.Name
			providerType = NodeProvider
		} else {
			srcProviderId = item.topologyKey + "=" + srcNode.Labels[item.topologyKey]
			dstProviderId = item.topologyKey + "=" + dstNode.Labels[item.topologyKey]
			providerType = NodeGroupProvider
		}
		am.BuildAffinityMaps(srcPodInfo.RequiredAntiAffinityTerms, srcPodInfo, dstPodInfo, dstNode, AntiAffinity)

		// Check the podMappings
		allSrcMappings := am.podMappings[util.PodKeyFunc(srcPodInfo.Pod)]
		allDstMappings := am.podMappings[util.PodKeyFunc(dstPodInfo.Pod)]
		srcMapping := PodMapping{
			MappingKey: MappingKey{
				CommodityKey: item.topologyKey + "|" + util.PodKeyFunc(srcPodInfo.Pod) + "|" + util.PodKeyFunc(dstPodInfo.Pod),
				MappingType:  AntiAffinitySrc,
			},
			Provider: Provider{
				ProviderType: providerType,
				ProviderId:   srcProviderId,
			},
		}
		_, exists1 := allSrcMappings[srcMapping]
		dstMapping := PodMapping{
			MappingKey: MappingKey{
				CommodityKey: item.topologyKey + "|" + util.PodKeyFunc(srcPodInfo.Pod) + "|" + util.PodKeyFunc(dstPodInfo.Pod),
				MappingType:  AntiAffinityDst,
			},
			Provider: Provider{
				ProviderType: providerType,
				ProviderId:   dstProviderId,
			},
		}
		_, exists2 := allDstMappings[dstMapping]
		assert.True(t, (exists1 && exists2) == item.expectExistence)

		// Check the ProviderMappings
		srcProviderMapping := am.providerMappings[srcProviderId]
		dstProviderMapping := am.providerMappings[dstProviderId]
		assert.True(t, srcProviderMapping.Keys.AntiAffinityKeys.Has(item.topologyKey+"|"+util.PodKeyFunc(srcPodInfo.Pod)+"|"+util.PodKeyFunc(dstPodInfo.Pod)))
		assert.True(t, dstProviderMapping.Keys.AntiAffinityKeys.Has(item.topologyKey+"|"+util.PodKeyFunc(srcPodInfo.Pod)+"|"+util.PodKeyFunc(dstPodInfo.Pod)))

	}

}
