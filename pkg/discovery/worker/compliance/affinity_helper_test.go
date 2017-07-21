package compliance

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/client-go/pkg/api/v1"
)

func TestMatchesSelector(t *testing.T) {
	table := []struct {
		node *api.Node
		pod  *api.Pod

		expectMatches bool
	}{
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			pod: &api.Pod{
				Spec: api.PodSpec{
					NodeSelector: map[string]string{
						"foo": "bar",
					},
				},
			},
			expectMatches: true,
		},
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			pod: &api.Pod{
				Spec: api.PodSpec{
					NodeSelector: map[string]string{
						"foo": "tar",
					},
				},
			},
			expectMatches: false,
		},
	}

	for i, item := range table {
		matches := matchesNodeSelector(item.pod, item.node)
		if matches != item.expectMatches {
			t.Errorf("Test case %d failed. Expected matches %t, got %t", i, item.expectMatches, matches)
		}
	}
}

func TestMatchesNodeAffinity(t *testing.T) {
	table := []struct {
		node *api.Node
		pod  *api.Pod

		expectMatches bool
	}{
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			pod: &api.Pod{
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						NodeAffinity: &api.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
								NodeSelectorTerms: []api.NodeSelectorTerm{
									{
										MatchExpressions: []api.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: api.NodeSelectorOpIn,
												Values:   []string{"bar", "value2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectMatches: true,
		},
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			pod: &api.Pod{
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						NodeAffinity: &api.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
								NodeSelectorTerms: []api.NodeSelectorTerm{
									{
										MatchExpressions: []api.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: api.NodeSelectorOpIn,
												Values:   []string{"value2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectMatches: false,
		},
	}

	for i, item := range table {
		matches := matchesNodeAffinity(item.pod, item.node)
		if matches != item.expectMatches {
			t.Errorf("Test case %d failed. Expected matches %t, got %t", i, item.expectMatches, matches)
		}
	}
}

func TestMatchesNodeSelectorTerms(t *testing.T) {
	table := []struct {
		node              *api.Node
		nodeSelectorTerms []api.NodeSelectorTerm

		expectsMatches bool
	}{
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":  "bar",
						"key1": "value1",
					},
				},
			},
			nodeSelectorTerms: []api.NodeSelectorTerm{
				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "foo",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"bar"},
						},
					},
				},
			},

			expectsMatches: true,
		},
		{
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":  "bar",
						"key1": "value1",
					},
				},
			},
			nodeSelectorTerms: []api.NodeSelectorTerm{
				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "foo",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"bar"},
						},
						{
							Key:      "key1",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"value1"},
						},
					},
				},
			},

			expectsMatches: true,
		},
		{
			// doesn't match.
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":  "bar",
						"key1": "value1",
					},
				},
			},
			nodeSelectorTerms: []api.NodeSelectorTerm{
				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "foo",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"bar"},
						},
						{
							Key:      "key1",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"value2"},
						},
					},
				},
			},

			expectsMatches: false,
		},
		{
			// invalid operator.
			node: &api.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":  "bar",
						"key1": "value1",
					},
				},
			},
			nodeSelectorTerms: []api.NodeSelectorTerm{
				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "foo",
							Operator: api.NodeSelectorOperator("invalid"),
							Values:   []string{"bar"},
						},
					},
				},
			},

			expectsMatches: false,
		},
	}

	for i, item := range table {
		matches := nodeMatchesNodeSelectorTerms(item.node, item.nodeSelectorTerms)
		if matches != item.expectsMatches {
			t.Errorf("Test case %d failed. Expects %t, got %t", i, item.expectsMatches, matches)
		}
	}
}

func TestAnyPodMatchesPodAffinityTerm(t *testing.T) {
	podLabel := map[string]string{"service": "securityscan"}
	podLabel2 := map[string]string{"security": "S1"}
	nodeLabels1 := map[string]string{
		"region": "r1",
		"zone":   "z11",
	}
	nodeLabels2 := map[string]string{
		"zone": "z22",
	}
	node1 := api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "machine1",
			Labels: nodeLabels1,
		},
	}

	term1 := api.PodAffinityTerm{
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
	}

	termWithNamespace := api.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "service",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"securityscan", "value2"},
				},
			},
		},
		Namespaces:  []string{"namespace2"},
		TopologyKey: "region",
	}

	table := []struct {
		pod          *api.Pod
		node         *api.Node
		podsNodesMap map[*api.Pod]*api.Node
		term         *api.PodAffinityTerm

		expectsMatches bool
		expectsErr     bool
	}{
		{
			// match test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						PodAffinity: &api.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
								term1,
							},
						},
					},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},
			term: &term1,

			expectsMatches: true,
			expectsErr:     false,
		},
		{
			// topology key mismatch
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						PodAffinity: &api.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
								termWithNamespace,
							},
						},
					},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Labels:    podLabel,
					Namespace: "namespace2",
				}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels2,
					},
				},
			},
			term: &term1,

			expectsMatches: false,
			expectsErr:     false,
		},
		{
			// topology key mismatch
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						PodAffinity: &api.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
								term1,
							},
						},
					},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels2,
					},
				},
			},
			term: &term1,

			expectsMatches: false,
			expectsErr:     false,
		},
		{
			// topology key mismatch
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						PodAffinity: &api.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
								term1,
							},
						},
					},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels2,
					},
				},
			},
			term: &term1,

			expectsMatches: false,
			expectsErr:     false,
		},
		{
			// namespace mismatch
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{
						PodAffinity: &api.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
								termWithNamespace,
							},
						},
					},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},
			term: &termWithNamespace,

			expectsMatches: false,
			expectsErr:     false,
		},
	}

	for i, item := range table {
		matches, _, err := anyPodMatchesPodAffinityTerm(item.pod, item.podsNodesMap, item.node, item.term)
		if err != nil {
			if !item.expectsErr {
				t.Errorf("Test case %d failed. Unexpected error: %s", i, err)
			}
		} else {
			if item.expectsErr {
				t.Errorf("Test case %d failed. Didn't get the expected error", i)
			} else {
				if matches != item.expectsMatches {
					t.Errorf("Test case %d failed. Expects %t, got %t.", i, item.expectsMatches, matches)
				}
			}
		}
	}
}

func TestSatisfiesPodsAffinityAntiAffinity(t *testing.T) {
	podLabel := map[string]string{"service": "securityscan"}
	podLabel2 := map[string]string{"security": "S1"}
	nodeLabels1 := map[string]string{
		"region": "r1",
		"zone":   "z11",
	}
	nodeLabels2 := map[string]string{
		"zone": "z22",
	}
	node1 := api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "machine1",
			Labels: nodeLabels1,
		},
	}

	term1 := api.PodAffinityTerm{
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
	}

	term2 := api.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "service",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"value2"},
				},
			},
		},
		TopologyKey: "region",
	}

	affinity1 := &api.Affinity{
		PodAffinity: &api.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term1,
			},
		},
	}
	affinity2 := &api.Affinity{
		PodAntiAffinity: &api.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term1,
			},
		},
	}

	affinity3 := &api.Affinity{
		PodAffinity: &api.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term2,
			},
		},
	}

	table := []struct {
		pod          *api.Pod
		node         *api.Node
		podsNodesMap map[*api.Pod]*api.Node
		affinity     *api.Affinity

		expectsMatches bool
	}{
		{
			// match affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity1,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},
			affinity: affinity1,

			expectsMatches: true,
		},
		{
			// anti-affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity2,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},
			affinity: affinity2,

			expectsMatches: false,
		},
		{
			// find matching pod but term doesn't match
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity1,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels2,
					},
				},
			},
			affinity: affinity1,

			expectsMatches: false,
		},
		{
			// can't find match pod.
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity3,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},
			affinity: affinity3,

			expectsMatches: false,
		},
	}

	for i, item := range table {
		matches := satisfiesPodsAffinityAntiAffinity(item.pod, item.node, item.affinity, item.podsNodesMap)
		if matches != item.expectsMatches {
			t.Errorf("Test case %d failed. Expects %t, got %t.", i, item.expectsMatches, matches)
		}
	}
}

func TestInterPodAffinityMatches(t *testing.T) {
	podLabel := map[string]string{"service": "securityscan"}
	podLabel2 := map[string]string{"security": "S1"}
	nodeLabels1 := map[string]string{
		"region": "r1",
		"zone":   "z11",
	}
	nodeLabels2 := map[string]string{
		"zone": "z22",
	}
	node1 := api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "machine1",
			Labels: nodeLabels1,
		},
	}

	term1 := api.PodAffinityTerm{
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
	}

	term2 := api.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "service",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"value2"},
				},
			},
		},
		TopologyKey: "region",
	}

	affinity1 := &api.Affinity{
		PodAffinity: &api.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term1,
			},
		},
	}
	affinity2 := &api.Affinity{
		PodAntiAffinity: &api.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term1,
			},
		},
	}

	affinity3 := &api.Affinity{
		PodAffinity: &api.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				term2,
			},
		},
	}

	table := []struct {
		pod          *api.Pod
		node         *api.Node
		podsNodesMap map[*api.Pod]*api.Node

		expectsMatches bool
	}{
		{
			// match affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity1,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},

			expectsMatches: true,
		},
		{
			// anti-affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity2,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},

			expectsMatches: false,
		},
		{
			// find matching pod but term doesn't match
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity1,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels2,
					},
				},
			},

			expectsMatches: false,
		},
		{
			// can't find match pod.
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: affinity3,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},

			expectsMatches: false,
		},
		{
			// match affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},

			expectsMatches: true,
		},
		{
			// match affinity rule test
			pod: &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabel2,
				},
				Spec: api.PodSpec{
					Affinity: &api.Affinity{},
				},
			},
			node: &node1,
			podsNodesMap: map[*api.Pod]*api.Node{
				{ObjectMeta: metav1.ObjectMeta{Labels: podLabel}}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:   "machine2",
						Labels: nodeLabels1,
					},
				},
			},

			expectsMatches: true,
		},
	}

	for i, item := range table {
		matches := interPodAffinityMatches(item.pod, item.node, item.podsNodesMap)
		if matches != item.expectsMatches {
			t.Errorf("Test case %d failed. Expects %t, got %t.", i, item.expectsMatches, matches)
		}
	}
}
