package testing

import (
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"testing"
)

// These are tests for the two new add/remove expression functions we've added ourselves.  This doesn't test other
// existing functions directly copied from upstream.
var (
	expr1 = v1.NodeSelectorRequirement{
		Key:      "kubernetes.io/hostname",
		Operator: v1.NodeSelectorOpIn,
		Values:   []string{"xyz"},
	}
	expr2 = v1.NodeSelectorRequirement{
		Key:      "dragon",
		Operator: v1.NodeSelectorOpDoesNotExist,
	}
	expr3 = v1.NodeSelectorRequirement{
		Key:      "abc",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   []string{"foo", "bar"},
	}
)

// TestNodeSelectorWrapper_AddToEmpty tests adding an expression to an empty node selector.
func TestNodeSelectorWrapper_AddToEmpty(t *testing.T) {
	actual := *MakeNodeSelector().AddExpressionToAll(expr1).Obj()
	assert.Equal(t, v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
		},
	}, actual)
}

// TestNodeSelectorWrapper_AddToSingleList tests adding an expression to a node selector with a single list.
func TestNodeSelectorWrapper_AddToSingleList(t *testing.T) {
	nodeSelector := v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
		},
	}
	actual := *MakeNodeSelectorWrapper(&nodeSelector).AddExpressionToAll(expr2).Obj()
	assert.Equal(t, v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2}},
		},
	}, actual)
}

// TestNodeSelectorWrapper_AddToTwoLists tests adding an expression to a node selector with two lists.
func TestNodeSelectorWrapper_AddToTwoLists(t *testing.T) {
	nodeSelector := v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1},
			},
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr2},
			},
		},
	}
	actual := *MakeNodeSelectorWrapper(&nodeSelector).AddExpressionToAll(expr3).Obj()
	assert.Equal(t, v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr3},
			},
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr2, expr3},
			},
		},
	}, actual)
}

// TestNodeSelectorWrapper_RemoveFromEmpty tests removing an expression from an empty node selector.
func TestNodeSelectorWrapper_RemoveFromEmpty(t *testing.T) {
	assert.Nil(t, MakeNodeSelector().RemoveExpressionFromAll(expr1).Obj())
}

// TestNodeSelectorWrapper_RemoveFromSingleton tests removing an expression from a singleton node selector.
func TestNodeSelectorWrapper_RemoveFromSingleton(t *testing.T) {
	nodeSelector := v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr1}},
		},
	}
	assert.Nil(t, MakeNodeSelectorWrapper(&nodeSelector).RemoveExpressionFromAll(expr1).Obj())
}

// TestNodeSelectorWrapper_RemoveFromAListOfTwo tests removing an expression from a list of two.
func TestNodeSelectorWrapper_RemoveFromAListOfTwo(t *testing.T) {
	nodeSelector := v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1, expr2},
			},
		},
	}
	actual := *MakeNodeSelectorWrapper(&nodeSelector).RemoveExpressionFromAll(expr1).Obj()
	assert.Equal(t, v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr2}},
		},
	}, actual)
}

// TestNodeSelectorWrapper_RemoveFromTwoLists tests removing an expression from two lists.
func TestNodeSelectorWrapper_RemoveFromTwoLists(t *testing.T) {
	nodeSelector := v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr1},
			},
			{
				MatchExpressions: []v1.NodeSelectorRequirement{expr2},
			},
		},
	}
	actual := *MakeNodeSelectorWrapper(&nodeSelector).RemoveExpressionFromAll(expr1).Obj()
	assert.Equal(t, v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{{
			MatchExpressions: []v1.NodeSelectorRequirement{expr2}},
		},
	}, actual)
}
