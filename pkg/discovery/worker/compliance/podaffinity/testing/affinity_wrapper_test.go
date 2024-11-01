package testing

import (
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

// TestAffinityWrapper_AddToEmpty tests adding an expression to an empty affinity.
func TestAffinityWrapper_AddToEmpty(t *testing.T) {
	actual := *MakeAffinity().AddNodeAffinityExpressionToAllRequired(expr1).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_AddToSingleList tests adding an expression to an affinity with a single required list.
func TestAffinityWrapper_AddToSingleList(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).AddNodeAffinityExpressionToAllRequired(expr2).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_AddToTwoLists tests adding an expression to an affinity with two lists.
func TestAffinityWrapper_AddToTwoLists(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr2},
					},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).AddNodeAffinityExpressionToAllRequired(expr3).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr3},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr2, expr3},
					},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_RemoveFromEmpty tests removing an expression from an empty affinity.
func TestAffinityWrapper_RemoveFromEmpty(t *testing.T) {
	assert.Nil(t, MakeAffinity().RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj())
}

// TestAffinityWrapper_RemoveFromSingleton tests removing an expression from a singleton affinity.
func TestAffinityWrapper_RemoveFromSingleton(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
				},
			},
		},
	}
	assert.Nil(t, MakeAffinityWrapper(&affinity).RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj())
}

// TestAffinityWrapper_RemoveFromAListOfTwo tests removing an expression from a list of two.
func TestAffinityWrapper_RemoveFromAListOfTwo(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{expr2}},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_RemoveFromTwoLists tests removing an expression from two lists.
func TestAffinityWrapper_RemoveFromTwoLists(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr3},
					},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr2},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr3},
					},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_RemoveNodeAffinityNotAffectingPodAffinity tests removing node affinity not affecting pod affinity.
func TestAffinityWrapper_RemoveNodeAffinityNotAffectingPodAffinity(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr3},
					},
				},
			},
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
				{
					Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
				},
				{
					Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr3}},
				},
			},
		},
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr2},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr3},
					},
				},
			},
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
				{
					Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
				},
				{
					Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr3}},
				},
			},
		},
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}, actual)
}

// TestAffinityWrapper_RemoveNodeAffinityToEmptyNotAffectingPodAffinity tests removing the last required node affinity
// term does not affecting other affinity terms such as the pod affinity.
func TestAffinityWrapper_RemoveNodeAffinityToEmptyNotAffectingPodAffinity(t *testing.T) {
	affinity := v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1},
					},
					{
						MatchExpressions: []v1.NodeSelectorRequirement{expr1},
					},
				},
			},
		},
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}
	actual := *MakeAffinityWrapper(&affinity).RemoveNodeAffinityExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.Affinity{
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}, actual)
}
