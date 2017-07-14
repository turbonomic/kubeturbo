package compliance

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	api "k8s.io/client-go/pkg/api/v1"
)

// NodeSelectorRequirementsAsSelector converts the []NodeSelectorRequirement api type into a struct that implements
// labels.Selector.
func NodeSelectorRequirementsAsSelector(nsm []api.NodeSelectorRequirement) (labels.Selector, error) {
	if len(nsm) == 0 {
		return labels.Nothing(), nil
	}
	selector := labels.NewSelector()
	for _, expr := range nsm {
		var op selection.Operator
		switch expr.Operator {
		case api.NodeSelectorOpIn:
			op = selection.In
		case api.NodeSelectorOpNotIn:
			op = selection.NotIn
		case api.NodeSelectorOpExists:
			op = selection.Exists
		case api.NodeSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		case api.NodeSelectorOpGt:
			op = selection.GreaterThan
		case api.NodeSelectorOpLt:
			op = selection.LessThan
		default:
			return nil, fmt.Errorf("%q is not a valid node selector operator", expr.Operator)
		}
		r, err := labels.NewRequirement(expr.Key, op, expr.Values)
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*r)
	}
	return selector, nil
}
