package compliance

import (
	"errors"
	"fmt"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
)

// NOTE: implementations are copied and modified from scheduler/predicate

//----------------------------------------- Node Affinity -------------------------------------------------------

func matchesNodeSelector(pod *api.Pod, node *api.Node) bool {
	// Check if node.Labels match pod.Spec.NodeSelector.
	if len(pod.Spec.NodeSelector) > 0 {
		selector := labels.SelectorFromSet(pod.Spec.NodeSelector)
		if !selector.Matches(labels.Set(node.Labels)) {
			return false
		}
	}
	return true
}

// The pod can only schedule onto nodes that satisfy requirements in both NodeAffinity and nodeSelector.
func matchesNodeAffinity(pod *api.Pod, node *api.Node) bool {
	nodeAffinityMatches := true
	affinity := pod.Spec.Affinity
	if affinity != nil && affinity.NodeAffinity != nil {
		nodeAffinity := affinity.NodeAffinity
		// if no required NodeAffinity requirements, will do no-op, means select all nodes.
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			return true
		}

		// Match node selector for requiredDuringSchedulingIgnoredDuringExecution.
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			nodeSelectorTerms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			glog.V(10).Infof("Match for RequiredDuringSchedulingIgnoredDuringExecution node selector terms %+v", nodeSelectorTerms)
			nodeAffinityMatches = nodeAffinityMatches && nodeMatchesNodeSelectorTerms(node, nodeSelectorTerms)
		}
	}
	return nodeAffinityMatches
}

// nodeMatchesNodeSelectorTerms checks if a node's labels satisfy a list of node selector terms,
// terms are ORed, and an empty list of terms will match nothing.
func nodeMatchesNodeSelectorTerms(node *api.Node, nodeSelectorTerms []api.NodeSelectorTerm) bool {
	for _, req := range nodeSelectorTerms {
		nodeSelector, err := nodeSelectorRequirementsAsSelector(req.MatchExpressions)
		if err != nil {
			glog.V(10).Infof("Failed to parse MatchExpressions: %+v, regarding as not match.", req.MatchExpressions)
			return false
		}
		if nodeSelector.Matches(labels.Set(node.Labels)) {
			return true
		}
	}
	return false
}

//----------------------------------------- Pod Affinity -------------------------------------------------------

func interPodAffinityMatches(pod *api.Pod, node *api.Node, allPodsNodesMap map[*api.Pod]*api.Node) bool {
	if !satisfiesExistingPodsAntiAffinity(pod, node, allPodsNodesMap) {
		return false
	}

	// Now check if <pod> requirements will be satisfied on this node.
	affinity := pod.Spec.Affinity
	if affinity == nil || (affinity.PodAffinity == nil && affinity.PodAntiAffinity == nil) {
		return true
	}
	if !satisfiesPodsAffinityAntiAffinity(pod, node, affinity, allPodsNodesMap) {
		return false
	}

	return true
}

func satisfiesExistingPodsAntiAffinity(pod *api.Pod, node *api.Node, allPodsNodesMap map[*api.Pod]*api.Node) bool {
	matchingTerms, err := getMatchingAntiAffinityTerms(pod, allPodsNodesMap)
	if err != nil {
		glog.Errorf("Failed to get all terms that pod %+v matches, err: %+v", util.GetPodClusterID(pod), err)
		return false
	}
	for _, term := range matchingTerms {
		if len(term.term.TopologyKey) == 0 {
			glog.Error("Empty topologyKey is not allowed except for PreferredDuringScheduling pod anti-affinity")
			return false
		}
		if nodesHaveSameTopologyKey(node, term.node, term.term.TopologyKey) {
			glog.V(10).Infof("Cannot schedule pod %+v onto node %v,because of PodAntiAffinityTerm %v",
				util.GetPodClusterID(pod), node.Name, term.term)
			return false
		}
	}
	return true
}

type matchingPodAntiAffinityTerm struct {
	term *api.PodAffinityTerm
	node *api.Node
}

func getMatchingAntiAffinityTerms(pod *api.Pod, allPodsNodesMap map[*api.Pod]*api.Node) ([]matchingPodAntiAffinityTerm, error) {
	var result []matchingPodAntiAffinityTerm
	for existingPod, existingPodNode := range allPodsNodesMap {
		// Skip self as allPodsNodesMap contains this pod also
		if pod.Name == existingPod.Name && pod.Namespace == existingPod.Namespace {
			continue
		}
		affinity := existingPod.Spec.Affinity
		if affinity != nil && affinity.PodAntiAffinity != nil {
			for _, term := range getPodAntiAffinityTerms(affinity.PodAntiAffinity) {
				namespaces := getNamespacesFromPodAffinityTerm(existingPod, &term)
				selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
				if err != nil {
					return nil, err
				}
				if podMatchesTermsNamespaceAndSelector(pod, namespaces, selector) {
					result = append(result, matchingPodAntiAffinityTerm{term: &term, node: existingPodNode})
				}
			}
		}
	}
	return result, nil
}

func satisfiesPodsAffinityAntiAffinity(pod *api.Pod, node *api.Node, affinity *api.Affinity, allPodsNodesMap map[*api.Pod]*api.Node) bool {

	// Check all affinity terms.
	for _, term := range getPodAffinityTerms(affinity.PodAffinity) {
		termMatches, matchingPodExists, err := anyPodMatchesPodAffinityTerm(pod, allPodsNodesMap, node, &term)
		if err != nil {
			glog.Errorf("Cannot schedule pod %+v onto node %v,because of PodAffinityTerm %v, err: %v",
				util.GetPodClusterID(pod), node.Name, term, err)
			return false
		}
		if !termMatches {
			// If the requirement matches a pod's own labels and namespace, and there are
			// no other such pods, then disregard the requirement. This is necessary to
			// not block forever because the first pod of the collection can't be scheduled.
			if matchingPodExists {
				glog.V(10).Infof("Cannot schedule pod %+v onto node %v,because of PodAffinityTerm %v, err: %v",
					util.GetPodClusterID(pod), node.Name, term, err)
				return false
			}
			namespaces := getNamespacesFromPodAffinityTerm(pod, &term)
			selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
			if err != nil {
				glog.Errorf("Cannot parse selector on term %v for pod %v. Details %v",
					term, util.GetPodClusterID(pod), err)
				return false
			}
			match := podMatchesTermsNamespaceAndSelector(pod, namespaces, selector)
			if !match {
				glog.V(10).Infof("Cannot schedule pod %+v onto node %v,because of PodAffinityTerm %v, err: %v",
					util.GetPodClusterID(pod), node.Name, term, err)
				return false
			}
		}
	}

	// Check all anti-affinity terms.
	for _, term := range getPodAntiAffinityTerms(affinity.PodAntiAffinity) {
		termMatches, _, err := anyPodMatchesPodAffinityTerm(pod, allPodsNodesMap, node, &term)
		if err != nil || termMatches {
			glog.V(10).Infof("Cannot schedule pod %+v onto node %v,because of PodAntiAffinityTerm %v, err: %v",
				util.GetPodClusterID(pod), node.Name, term, err)
			return false
		}
	}

	return true
}

func anyPodMatchesPodAffinityTerm(pod *api.Pod, allPodsNodesMap map[*api.Pod]*api.Node, node *api.Node, term *api.PodAffinityTerm) (bool, bool, error) {
	if len(term.TopologyKey) == 0 {
		return false, false, errors.New("Empty topologyKey is not allowed except for PreferredDuringScheduling pod anti-affinity")
	}
	matchingPodExists := false
	namespaces := getNamespacesFromPodAffinityTerm(pod, term)
	selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
	if err != nil {
		return false, false, err
	}
	for existingPod, existingPodNode := range allPodsNodesMap {
		// Skip self as allPodsNodesMap contains this pod also
		if pod.Name == existingPod.Name && pod.Namespace == existingPod.Namespace {
			continue
		}
		match := podMatchesTermsNamespaceAndSelector(existingPod, namespaces, selector)
		if match {
			matchingPodExists = true
			if nodesHaveSameTopologyKey(node, existingPodNode, term.TopologyKey) {
				return true, matchingPodExists, nil
			}
		}
	}
	return false, matchingPodExists, nil
}

//----------------------------------------- util -------------------------------------------------------

func getPodAntiAffinityTerms(podAntiAffinity *api.PodAntiAffinity) (terms []api.PodAffinityTerm) {
	if podAntiAffinity != nil {
		if len(podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 0 {
			terms = podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		}
	}
	return terms
}

func getNamespacesFromPodAffinityTerm(pod *api.Pod, podAffinityTerm *api.PodAffinityTerm) map[string]struct{} {
	names := make(map[string]struct{})
	if len(podAffinityTerm.Namespaces) == 0 {
		names[pod.Namespace] = struct{}{}
	} else {
		for _, namespace := range podAffinityTerm.Namespaces {
			names[namespace] = struct{}{}
		}
	}
	return names
}

func podMatchesTermsNamespaceAndSelector(pod *api.Pod, namespaces map[string]struct{}, selector labels.Selector) bool {
	if _, exist := namespaces[pod.Namespace]; !exist {
		return false
	}

	if !selector.Matches(labels.Set(pod.Labels)) {
		return false
	}
	return true
}

func nodesHaveSameTopologyKey(nodeA, nodeB *api.Node, topologyKey string) bool {
	if len(topologyKey) == 0 {
		return false
	}

	if nodeA.Labels == nil || nodeB.Labels == nil {
		return false
	}

	nodeALabel, okA := nodeA.Labels[topologyKey]
	nodeBLabel, okB := nodeB.Labels[topologyKey]

	// If found label in both nodes, check the label
	if okB && okA {
		return nodeALabel == nodeBLabel
	}

	return false
}

// NodeSelectorRequirementsAsSelector converts the []NodeSelectorRequirement api type into a struct that implements
// labels.Selector.
func nodeSelectorRequirementsAsSelector(nsm []api.NodeSelectorRequirement) (labels.Selector, error) {
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

func getPodAffinityTerms(podAffinity *api.PodAffinity) (terms []api.PodAffinityTerm) {
	if podAffinity != nil {
		if len(podAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 0 {
			terms = podAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		}
		// TODO: Uncomment this block when implement RequiredDuringSchedulingRequiredDuringExecution.
		//if len(podAffinity.RequiredDuringSchedulingRequiredDuringExecution) != 0 {
		//	terms = append(terms, podAffinity.RequiredDuringSchedulingRequiredDuringExecution...)
		//}
	}
	return terms
}
