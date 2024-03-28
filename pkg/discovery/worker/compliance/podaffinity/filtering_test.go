package podaffinity

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listersv1 "k8s.io/client-go/listers/core/v1"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	at "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity/testing"
)

var (
	defaultNamespace = ""
	ns1              = "ns1"
)

var nsLabelT1 = map[string]string{"team": "team1"}
var nsLabelT2 = map[string]string{"team": "team2"}
var namespaces = []*v1.Namespace{
	{ObjectMeta: metav1.ObjectMeta{Name: "subteam1.team1", Labels: nsLabelT1}},
	{ObjectMeta: metav1.ObjectMeta{Name: "subteam2.team1", Labels: nsLabelT1}},
	{ObjectMeta: metav1.ObjectMeta{Name: "subteam1.team2", Labels: nsLabelT2}},
	{ObjectMeta: metav1.ObjectMeta{Name: "subteam2.team2", Labels: nsLabelT2}},
}

type fakeNSLister struct {
	namespaces []*v1.Namespace
}

func NewFakeNSLister(namespaces []*v1.Namespace) listersv1.NamespaceLister {
	return &fakeNSLister{
		namespaces: namespaces,
	}
}

func (f *fakeNSLister) List(selector labels.Selector) (ret []*v1.Namespace, err error) {
	for _, ns := range f.namespaces {
		if selector.Matches(labels.Set(ns.GetLabels())) {
			ret = append(ret, ns)
		}
	}
	return ret, nil
}

func (f *fakeNSLister) Get(name string) (*v1.Namespace, error) {
	for _, ns := range f.namespaces {
		if ns.Name == name {
			return ns, nil
		}
	}

	return nil, fmt.Errorf("Namespace does not exist")
}

func TestRequiredAffinitySingleNode(t *testing.T) {
	podLabel := map[string]string{"service": "securityscan"}
	pod := at.MakePod().Name("pod1").Namespace(ns1).Labels(podLabel).Node("node1").Obj()

	labels1 := map[string]string{
		"region": "r1",
		"zone":   "z11",
	}
	podLabel2 := map[string]string{"security": "S1"}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: labels1}}

	tests := []struct {
		name                string
		podToPlace          *v1.Pod
		pods                []*v1.Pod
		expectPlaced        bool
		expectPrefilterFail bool
	}{
		{
			name:         "A pod that has no required pod affinity scheduling rules can schedule onto a node with no existing pods",
			podToPlace:   new(v1.Pod),
			expectPlaced: true,
		},
		{
			name:         "satisfies the pod with requiredDuringSchedulingIgnoredDuringExecution in PodAffinity using In operator that matches the existing pod",
			podToPlace:   at.MakePod().Name("podToPlace").Namespace(ns1).Labels(podLabel2).PodAffinityIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{pod},
			expectPlaced: true,
		},
		{
			name:         "satisfies the pod with requiredDuringSchedulingIgnoredDuringExecution in PodAffinity using not in operator in labelSelector that matches the existing pod",
			podToPlace:   at.MakePod().Name("podToPlace").Namespace(ns1).Labels(podLabel2).PodAffinityNotIn("service", "region", []string{"securityscan3", "value3"}, at.PodAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{pod},
			expectPlaced: true,
		},
		{
			name: "Does not satisfy the PodAffinity with labelSelector because of diff Namespace",
			podToPlace: createPodWithAffinityTerms(defaultNamespace, "", "podToPlace", podLabel2,
				[]v1.PodAffinityTerm{
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
						Namespaces: []string{"DiffNameSpace"},
					},
				}, nil),
			pods:         []*v1.Pod{at.MakePod().Namespace("ns").Label("service", "securityscan").Node("node1").Obj()},
			expectPlaced: false,
		},
		{
			name:         "Doesn't satisfy the PodAffinity because of unmatching labelSelector with the existing pod",
			podToPlace:   at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).PodAffinityIn("service", "", []string{"antivirusscan", "value2"}, at.PodAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{pod},
			expectPlaced: false,
		},
		{
			name: "satisfies the PodAffinity with different label Operators in multiple RequiredDuringSchedulingIgnoredDuringExecution ",
			podToPlace: createPodWithAffinityTerms(ns1, "", "podToPlace", podLabel2,
				[]v1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "service",
									Operator: metav1.LabelSelectorOpExists,
								}, {
									Key:      "wrongkey",
									Operator: metav1.LabelSelectorOpDoesNotExist,
								},
							},
						},
						TopologyKey: "region",
					}, {
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "service",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"securityscan"},
								}, {
									Key:      "service",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{"WrongValue"},
								},
							},
						},
						TopologyKey: "region",
					},
				}, nil),
			pods:         []*v1.Pod{pod},
			expectPlaced: true,
		},
		{
			name: "The labelSelector requirements(items of matchExpressions) are ANDed, the pod cannot schedule onto the node because one of the matchExpression item don't match.",
			podToPlace: createPodWithAffinityTerms(defaultNamespace, "", "podToPlace", podLabel2,
				[]v1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "service",
									Operator: metav1.LabelSelectorOpExists,
								}, {
									Key:      "wrongkey",
									Operator: metav1.LabelSelectorOpDoesNotExist,
								},
							},
						},
						TopologyKey: "region",
					}, {
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "service",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"securityscan2"},
								}, {
									Key:      "service",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{"WrongValue"},
								},
							},
						},
						TopologyKey: "region",
					},
				}, nil),
			pods:         []*v1.Pod{pod},
			expectPlaced: false,
		},
		{
			name: "satisfies the PodAffinity and PodAntiAffinity with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(ns1).Labels(podLabel2).
				PodAffinityIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).
				PodAntiAffinityIn("service", "node", []string{"antivirusscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{pod},
			expectPlaced: true,
		},
		{
			name: "satisfies the PodAffinity and PodAntiAffinity and PodAntiAffinity symmetry with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel2).
				PodAffinityIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).
				PodAntiAffinityIn("service", "node", []string{"antivirusscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel).
					PodAntiAffinityIn("service", "node", []string{"antivirusscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: true,
		},
		{
			name: "satisfies the PodAffinity but doesn't satisfy the PodAntiAffinity with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel2).
				PodAffinityIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).
				PodAntiAffinityIn("service", "zone", []string{"securityscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{pod},
			expectPlaced: false,
		},
		{
			name: "satisfies the PodAffinity and PodAntiAffinity but doesn't satisfy PodAntiAffinity symmetry with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAffinityIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).
				PodAntiAffinityIn("service", "node", []string{"antivirusscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Labels(podLabel).Node("node1").PodAntiAffinityIn("service", "zone", []string{"securityscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "pod matches its own Label in PodAffinity and that matches the existing pod Labels",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAffinityNotIn("service", "region", []string{"securityscan", "value2"}, at.PodAffinityWithRequiredReq).Obj(),
			pods:         []*v1.Pod{at.MakePod().Name("pod1").Label("service", "securityscan").Node("node2").Obj()},
			expectPlaced: false,
		},
		{
			name:       "verify that PodAntiAffinity from existing pod is respected when pod has no AntiAffinity constraints. doesn't satisfy PodAntiAffinity symmetry with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Labels(podLabel).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel).
					PodAntiAffinityIn("service", "zone", []string{"securityscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name:       "verify that PodAntiAffinity from existing pod is respected when pod has no AntiAffinity constraints. satisfy PodAntiAffinity symmetry with the existing pod",
			podToPlace: at.MakePod().Name("podToPlace").Labels(podLabel).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel).
					PodAntiAffinityNotIn("service", "zone", []string{"securityscan", "value2"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: true,
		},
		{
			name: "satisfies the PodAntiAffinity with existing pod but doesn't satisfy PodAntiAffinity symmetry with incoming pod",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAntiAffinityExists("service", "region", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("security", "region", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel2).
					PodAntiAffinityExists("security", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "PodAntiAffinity symmetry check a1: incoming pod and existing pod partially match each other on AffinityTerms",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAntiAffinityExists("service", "zone", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("security", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel2).
					PodAntiAffinityExists("security", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "PodAntiAffinity symmetry check a2: incoming pod and existing pod partially match each other on AffinityTerms",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel2).
				PodAntiAffinityExists("security", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(podLabel).
					PodAntiAffinityExists("service", "zone", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("security", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "PodAntiAffinity symmetry check b1: incoming pod and existing pod partially match each other on AffinityTerms",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(map[string]string{"abc": "", "xyz": ""}).
				PodAntiAffinityExists("abc", "zone", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("def", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(map[string]string{"def": "", "xyz": ""}).
					PodAntiAffinityExists("abc", "zone", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("def", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "PodAntiAffinity symmetry check b2: incoming pod and existing pod partially match each other on AffinityTerms",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(map[string]string{"def": "", "xyz": ""}).
				PodAntiAffinityExists("abc", "zone", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("def", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Namespace(defaultNamespace).Node("node1").Labels(map[string]string{"abc": "", "xyz": ""}).
					PodAntiAffinityExists("abc", "zone", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("def", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			expectPlaced: false,
		},
		{
			name: "PodAffinity fails PreFilter with an invalid affinity label syntax",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAffinityIn("service", "region", []string{"{{.bad-value.}}"}, at.PodAffinityWithRequiredReq).
				PodAffinityIn("service", "node", []string{"antivirusscan", "value2"}, at.PodAffinityWithRequiredReq).Obj(),
			expectPlaced:        false,
			expectPrefilterFail: true,
		},
		{
			name: "PodAntiAffinity fails PreFilter with an invalid antiaffinity label syntax",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(podLabel).
				PodAffinityIn("service", "region", []string{"foo"}, at.PodAffinityWithRequiredReq).
				PodAffinityIn("service", "node", []string{"{{.bad-value.}}"}, at.PodAffinityWithRequiredReq).Obj(),
			expectPlaced:        false,
			expectPrefilterFail: true,
		},
		{
			name: "affinity with NamespaceSelector",
			podToPlace: createPodWithAffinityTerms(defaultNamespace, "", "podToPlace", podLabel2,
				[]v1.PodAffinityTerm{
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
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "team",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"team1"},
								},
							},
						},
						TopologyKey: "region",
					},
				}, nil),
			pods:         []*v1.Pod{{Spec: v1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "subteam1.team1", Labels: podLabel}}},
			expectPlaced: true,
		},
		{
			name: "affinity with non-matching NamespaceSelector",
			podToPlace: createPodWithAffinityTerms(ns1, "", "podToPlace", podLabel2,
				[]v1.PodAffinityTerm{
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
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "team",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"team1"},
								},
							},
						},
						TopologyKey: "region",
					},
				}, nil),
			pods:         []*v1.Pod{{Spec: v1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "subteam1.team2", Labels: podLabel}}},
			expectPlaced: false,
		},
		{
			name: "anti-affinity with matching NamespaceSelector",
			podToPlace: createPodWithAffinityTerms("subteam1.team1", "", "podToPlace", podLabel2, nil,
				[]v1.PodAffinityTerm{
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
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "team",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"team1"},
								},
							},
						},
						TopologyKey: "zone",
					},
				}),
			pods:         []*v1.Pod{{Spec: v1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "subteam2.team1", Name: "pod1", Labels: podLabel}}},
			expectPlaced: false,
		},
		{
			name: "anti-affinity with matching all NamespaceSelector",
			podToPlace: createPodWithAffinityTerms("subteam1.team1", "", "podToPlace", podLabel2, nil,
				[]v1.PodAffinityTerm{
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
						NamespaceSelector: &metav1.LabelSelector{},
						TopologyKey:       "zone",
					},
				}),
			pods:         []*v1.Pod{{Spec: v1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "subteam2.team1", Labels: podLabel}}},
			expectPlaced: false,
		},
		{
			name: "anti-affinity with non-matching NamespaceSelector",
			podToPlace: createPodWithAffinityTerms("subteam1.team1", "", "podToPlace", podLabel2, nil,
				[]v1.PodAffinityTerm{
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
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "team",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"team1"},
								},
							},
						},
						TopologyKey: "zone",
					},
				}),
			pods:         []*v1.Pod{{Spec: v1.PodSpec{NodeName: "node1"}, ObjectMeta: metav1.ObjectMeta{Namespace: "subteam1.team2", Labels: podLabel}}},
			expectPlaced: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			pods := []*v1.Pod{}
			pods = append(pods, test.pods...)
			clusterSummary := createTestClusterSummary(pods, []*v1.Node{node})
			pr, err := New(clusterSummary,
				NewNodeInfoLister(clusterSummary), NewFakeNSLister(namespaces))
			if err != nil {
				t.Errorf("Error creating new processor: %v", err)
				return
			}
			nodeInfos, err := pr.nodeInfoLister.List()
			if err != nil {
				t.Errorf("Error retreiving nodeinfos while processing affinities, %V.", err)
				return
			}

			podToPlace := test.podToPlace
			qualifiedPodName := podToPlace.Namespace + "/" + podToPlace.Name
			state, err := pr.PreFilter(ctx, podToPlace)
			if err != nil {
				if test.expectPrefilterFail {
					return
				}
				t.Errorf("Error computing prefilter state for pod, %s.", qualifiedPodName)
			}

			err = pr.Filter(ctx, state, podToPlace, nodeInfos[0])
			// Err means a placement was not found on this node
			if err != nil && test.expectPlaced {
				t.Errorf("Pod %s was expected to be placed on %s", podToPlace.Name, nodeInfos[0].node.Name)
			} else if err == nil && !test.expectPlaced {
				t.Errorf("Pod %s was NOT expected to be placed on %s", podToPlace.Name, nodeInfos[0].node.Name)
			}
		})
	}
}

func TestRequiredAffinityMultipleNodes(t *testing.T) {
	podLabelA := map[string]string{
		"foo": "bar",
	}
	labelRgChina := map[string]string{
		"region": "China",
	}
	labelRgChinaAzAz1 := map[string]string{
		"region": "China",
		"az":     "az1",
	}
	labelRgIndia := map[string]string{
		"region": "India",
	}

	tests := []struct {
		name              string
		podToPlace        *v1.Pod
		pods              []*v1.Pod
		nodes             []*v1.Node
		expectPlacedOn    sets.String
		expectNotPlacedOn sets.String
	}{
		{
			name:       "A pod can be scheduled onto all the nodes that have the same topology key & label value with one of them has an existing pod that matches the affinity rules",
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAffinityIn("foo", "region", []string{"bar"}, at.PodAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("p1").Node("node1").Labels(podLabelA).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: labelRgChina}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: labelRgChinaAzAz1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: labelRgIndia}},
			},
			expectPlacedOn:    sets.NewString("node1", "node2"),
			expectNotPlacedOn: sets.NewString("node3"),
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(map[string]string{"foo": "bar", "service": "securityscan"}).
				PodAffinityIn("foo", "zone", []string{"bar"}, at.PodAffinityWithRequiredReq).
				PodAffinityIn("service", "zone", []string{"securityscan"}, at.PodAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("p1").Node("nodeA").Labels(map[string]string{"foo": "bar"}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"zone": "az1", "hostname": "h1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"zone": "az2", "hostname": "h2"}}},
			},
			expectPlacedOn: sets.NewString("nodeA", "nodeB"),
			name: "The affinity rule is to schedule all of the pods of this collection to the same zone. The first pod of the collection " +
				"should not be blocked from being scheduled onto any node, even there's no existing pod that matches the rule anywhere.",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).Labels(map[string]string{"foo": "bar", "service": "securityscan"}).
				PodAffinityIn("foo", "zone", []string{"bar"}, at.PodAffinityWithRequiredReq).
				PodAffinityIn("service", "zone", []string{"securityscan"}, at.PodAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("p1").Node("nodeA").Labels(map[string]string{"foo": "bar"}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"zoneLabel": "az1", "hostname": "h1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"zoneLabel": "az2", "hostname": "h2"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "The first pod of the collection can only be scheduled on nodes labelled with the requested topology keys",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityIn("foo", "region", []string{"abc"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": "abc"}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "NodeA and nodeB have same topologyKey and label value. NodeA has an existing pod that matches the inter pod affinity rule. The pod can not be scheduled onto nodeA and nodeB.",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityIn("foo", "region", []string{"abc"}, at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityIn("service", "zone", []string{"securityscan"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": "abc", "service": "securityscan"}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "This test ensures that anti-affinity matches a pod when any term of the anti-affinity rule matches a pod.",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityIn("foo", "region", []string{"abc"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": "abc"}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: labelRgChina}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: labelRgChinaAzAz1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeC", Labels: labelRgIndia}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			expectPlacedOn:    sets.NewString("nodeC"),
			name:              "NodeA and nodeB have same topologyKey and label value. NodeA has an existing pod that matches the inter pod affinity rule. The pod can not be scheduled onto nodeA and nodeB but can be scheduled onto nodeC",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace("NS1").Labels(map[string]string{"foo": "123"}).PodAntiAffinityIn("foo", "region", []string{"bar"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Namespace("NS1").Labels(map[string]string{"foo": "bar"}).Obj(),
				at.MakePod().Node("nodeC").Namespace("NS2").PodAntiAffinityIn("foo", "region", []string{"123"}, at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: labelRgChina}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: labelRgChinaAzAz1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeC", Labels: labelRgIndia}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			expectPlacedOn:    sets.NewString("nodeC"),
			name:              "NodeA and nodeB have same topologyKey and label value. NodeA has an existing pod that matches the inter pod affinity rule. The pod can not be scheduled onto nodeA, nodeB, but can be scheduled onto nodeC (NodeC has an existing pod that match the inter pod affinity rule but in different namespace)",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Label("foo", "").Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "invalid-node-label", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeB"}}},
			},
			expectPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:           "Test existing pod's anti-affinity: if an existing pod has a term with invalid topologyKey, labelSelector of the term is firstly checked, and then topologyKey of the term is also checked",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "invalid-node-label", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": ""}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeB"}}},
			},
			expectPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:           "Test incoming pod's anti-affinity: even if labelSelector matches, we still check if topologyKey matches",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Label("foo", "").Label("bar", "").Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
				at.MakePod().Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("bar", "region", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "Test existing pod's anti-affinity: incoming pod wouldn't considered as a fit as it violates each existingPod's terms on all nodes",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "zone", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("bar", "region", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": ""}).Obj(),
				at.MakePod().Node("nodeB").Labels(map[string]string{"bar": ""}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "Test incoming pod's anti-affinity: incoming pod wouldn't considered as a fit as it at least violates one anti-affinity rule of existingPod",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Label("foo", "").Label("bar", "").Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "invalid-node-label", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("bar", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA"),
			expectPlacedOn:    sets.NewString("nodeB"),
			name:              "Test existing pod's anti-affinity: only when labelSelector and topologyKey both match, it's counted as a single term match - case when one term has invalid topologyKey",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "invalid-node-label", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("bar", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("podA").Node("nodeA").Labels(map[string]string{"foo": "", "bar": ""}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA"),
			expectPlacedOn:    sets.NewString("nodeB"),
			name:              "Test incoming pod's anti-affinity: only when labelSelector and topologyKey both match, it's counted as a single term match - case when one term has invalid topologyKey",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Label("foo", "").Label("bar", "").Obj(),
			pods: []*v1.Pod{
				at.MakePod().Namespace(defaultNamespace).Node("nodeA").PodAntiAffinityExists("foo", "region", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("bar", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "Test existing pod's anti-affinity: only when labelSelector and topologyKey both match, it's counted as a single term match - case when all terms have valid topologyKey",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "region", at.PodAntiAffinityWithRequiredReq).
				PodAntiAffinityExists("bar", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Labels(map[string]string{"foo": "", "bar": ""}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:              "Test incoming pod's anti-affinity: only when labelSelector and topologyKey both match, it's counted as a single term match - case when all terms have valid topologyKey",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Label("foo", "").Label("bar", "").Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Namespace(defaultNamespace).PodAntiAffinityExists("foo", "zone", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("labelA", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
				at.MakePod().Node("nodeB").Namespace(defaultNamespace).PodAntiAffinityExists("bar", "zone", at.PodAntiAffinityWithRequiredReq).
					PodAntiAffinityExists("labelB", "zone", at.PodAntiAffinityWithRequiredReq).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeC", Labels: map[string]string{"region": "r1", "zone": "z3", "hostname": "nodeC"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeA", "nodeB"),
			expectPlacedOn:    sets.NewString("nodeC"),
			name:              "Test existing pod's anti-affinity: existingPod on nodeA and nodeB has at least one anti-affinity term matches incoming pod, so incoming pod can only be scheduled to nodeC",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAffinityExists("foo", "region", at.PodAffinityWithRequiredReq).
				PodAffinityExists("bar", "zone", at.PodAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Name("pod1").Labels(map[string]string{"foo": "", "bar": ""}).Node("nodeA").Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeB"}}},
			},
			expectPlacedOn: sets.NewString("nodeA", "nodeB"),
			name:           "Test incoming pod's affinity: firstly check if all affinityTerms match, and then check if all topologyKeys match",
		},
		{
			podToPlace: at.MakePod().Name("podToPlace").Namespace(defaultNamespace).PodAffinityExists("foo", "region", at.PodAffinityWithRequiredReq).
				PodAffinityExists("bar", "zone", at.PodAffinityWithRequiredReq).Obj(),
			pods: []*v1.Pod{
				at.MakePod().Node("nodeA").Name("pod1").Namespace(defaultNamespace).Labels(map[string]string{"foo": ""}).Obj(),
				at.MakePod().Node("nodeB").Name("pod2").Namespace(defaultNamespace).Labels(map[string]string{"bar": ""}).Obj(),
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeA", Labels: map[string]string{"region": "r1", "zone": "z1", "hostname": "nodeA"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "nodeB", Labels: map[string]string{"region": "r1", "zone": "z2", "hostname": "nodeB"}}},
			},
			expectNotPlacedOn: sets.NewString("nodeC"),
			name:              "Test incoming pod's affinity: firstly check if all affinityTerms match, and then check if all topologyKeys match, and the match logic should be satisfied on the same pod",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			pods := []*v1.Pod{}
			pods = append(pods, test.pods...)
			nodes := test.nodes
			clusterSummary := createTestClusterSummary(pods, nodes)
			pr, err := New(clusterSummary,
				NewNodeInfoLister(clusterSummary), NewFakeNSLister([]*v1.Namespace{
					{ObjectMeta: metav1.ObjectMeta{Name: "NS1"}},
				}))
			if err != nil {
				t.Errorf("Error creating new processor: %v", err)
				return
			}
			nodeInfos, err := pr.nodeInfoLister.List()
			if err != nil {
				t.Errorf("Error retreiving nodeinfos while processing affinities, %V.", err)
				return
			}

			podToPlace := test.podToPlace
			qualifiedPodName := podToPlace.Namespace + "/" + podToPlace.Name
			state, err := pr.PreFilter(ctx, podToPlace)
			if err != nil {
				t.Errorf("Error computing prefilter state for pod, %s.", qualifiedPodName)
				return
			}

			for _, nodeInfo := range nodeInfos {
				err := pr.Filter(ctx, state, podToPlace, nodeInfo)
				// Err means a placement was not found on this node
				if err != nil && test.expectPlacedOn.Has(nodeInfo.node.Name) {
					t.Errorf("Pod %s was supposed to be placed on %s", podToPlace.Name, nodeInfo.node.Name)
				} else if err == nil && test.expectNotPlacedOn.Has(nodeInfo.node.Name) {
					t.Errorf("Pod %s was NOT supposed to be placed on %s", podToPlace.Name, nodeInfo.node.Name)
				}
			}
		})
	}
}

func createPodWithAffinityTerms(namespace, nodeName, name string, labels map[string]string, affinity, antiAffinity []v1.PodAffinityTerm) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    labels,
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Affinity: &v1.Affinity{
				PodAffinity: &v1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: affinity,
				},
				PodAntiAffinity: &v1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: antiAffinity,
				},
			},
		},
	}
}

func createTestClusterSummary(pods []*v1.Pod, nodes []*v1.Node) *repository.ClusterSummary {
	kubecluster := &repository.KubeCluster{
		Pods:  pods,
		Nodes: nodes,
	}
	cs := &repository.ClusterSummary{
		KubeCluster: kubecluster,
	}

	nodeToRunningPods := map[string][]*v1.Pod{}
	for _, pod := range pods {
		nodeToRunningPods[pod.Spec.NodeName] = append(nodeToRunningPods[pod.Spec.NodeName], pod)
	}
	cs.NodeToRunningPods = nodeToRunningPods

	return cs
}
