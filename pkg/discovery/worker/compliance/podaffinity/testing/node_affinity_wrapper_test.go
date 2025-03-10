package testing

import (
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"testing"
)

// TestNodeAffinityWrapper_AddToEmpty tests adding an expression to an empty node affinity.
func TestNodeAffinityWrapper_AddToEmpty(t *testing.T) {
	actual := *MakeNodeAffinity().AddExpressionToAllRequired(expr1).Obj()
	assert.Equal(t, v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
			},
		},
	}, actual)
}

// TestNodeAffinityWrapper_AddToSingleList tests adding an expression to a node affinity with a single required list.
func TestNodeAffinityWrapper_AddToSingleList(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
			},
		},
	}
	actual := *MakeNodeAffinityWrapper(&nodeAffinity).AddExpressionToAllRequired(expr2).Obj()
	assert.Equal(t, v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
			},
		},
	}, actual)
}

// TestNodeAffinityWrapper_AddToTwoLists tests adding an expression to a node affinity with two lists.
func TestNodeAffinityWrapper_AddToTwoLists(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
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
	}
	actual := *MakeNodeAffinityWrapper(&nodeAffinity).AddExpressionToAllRequired(expr3).Obj()
	assert.Equal(t, v1.NodeAffinity{
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
	}, actual)
}

// TestNodeAffinityWrapper_RemoveFromEmpty tests removing an expression from an empty node affinity.
func TestNodeAffinityWrapper_RemoveFromEmpty(t *testing.T) {
	assert.Nil(t, MakeNodeAffinity().RemoveExpressionFromAllRequired(expr1).Obj())
}

// TestNodeAffinityWrapper_RemoveFromSingleton tests removing an expression from a singleton node selector.
func TestNodeAffinityWrapper_RemoveFromSingleton(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
			},
		},
	}
	assert.Nil(t, MakeNodeAffinityWrapper(&nodeAffinity).RemoveExpressionFromAllRequired(expr1).Obj())
}

// TestNodeAffinityWrapper_RemoveFromAListOfTwo tests removing an expression from a list of two.
func TestNodeAffinityWrapper_RemoveFromAListOfTwo(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
			},
		},
	}
	actual := *MakeNodeAffinityWrapper(&nodeAffinity).RemoveExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr2}},
			},
		},
	}, actual)
}

// TestNodeAffinityWrapper_RemoveNotAffectingPreferredTerms tests removing (required) not affecting the preferred terms.
func TestNodeAffinityWrapper_RemoveNotAffectingPreferredTerms(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
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
		PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
			{
				Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
			},
		},
	}
	actual := *MakeNodeAffinityWrapper(&nodeAffinity).RemoveExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{expr2}},
			},
		},
		PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
			{
				Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
			},
		},
	}, actual)
}

// TestNodeAffinityWrapper_RemoveToEmptyRequiredNotAffectingPreferredTerms tests removing the last required term does
// not affect the preferred terms.
func TestNodeAffinityWrapper_RemoveToEmptyRequiredNotAffectingPreferredTerms(t *testing.T) {
	nodeAffinity := v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{
				{
					MatchExpressions: []v1.NodeSelectorRequirement{expr1},
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
	}
	actual := *MakeNodeAffinityWrapper(&nodeAffinity).RemoveExpressionFromAllRequired(expr1).Obj()
	assert.Equal(t, v1.NodeAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
			{
				Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
			},
			{
				Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{expr3}},
			},
		},
	}, actual)
}
