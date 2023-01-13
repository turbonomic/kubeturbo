package interpodaffinity

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

const (
	// ErrReasonExistingAntiAffinityRulesNotMatch is used for ExistingPodsAntiAffinityRulesNotMatch predicate error.
	ErrReasonExistingAntiAffinityRulesNotMatch = "node(s) didn't satisfy existing pods anti-affinity rules"
	// ErrReasonAffinityRulesNotMatch is used for PodAffinityRulesNotMatch predicate error.
	ErrReasonAffinityRulesNotMatch = "node(s) didn't match pod affinity rules"
	// ErrReasonAntiAffinityRulesNotMatch is used for PodAntiAffinityRulesNotMatch predicate error.
	ErrReasonAntiAffinityRulesNotMatch = "node(s) didn't match pod anti-affinity rules"
)

// preFilterState computed at PreFilter and used at Filter.
type preFilterState struct {
	// A map of topology pairs to the number of existing pods that has anti-affinity terms that match the "pod".
	existingAntiAffinityCounts topologyToMatchedTermCount
	// A map of topology pairs to the number of existing pods that match the affinity terms of the "pod".
	affinityCounts topologyToMatchedTermCount
	// A map of topology pairs to the number of existing pods that match the anti-affinity terms of the "pod".
	antiAffinityCounts topologyToMatchedTermCount
	// podInfo of the incoming pod.
	podInfo *PodInfo
	// A copy of the incoming pod's namespace labels.
	namespaceLabels labels.Set
}

// PreFilterResult wraps needed info for scheduler framework to act upon PreFilter phase.
type PreFilterResult struct {
	// The set of nodes that should be considered downstream; if nil then
	// all nodes are eligible.
	NodeNames sets.String
}

// Clone the prefilter state.
func (s *preFilterState) Clone() *preFilterState {
	if s == nil {
		return nil
	}

	copy := preFilterState{}
	copy.affinityCounts = s.affinityCounts.clone()
	copy.antiAffinityCounts = s.antiAffinityCounts.clone()
	copy.existingAntiAffinityCounts = s.existingAntiAffinityCounts.clone()
	// No need to deep copy the podInfo because it shouldn't change.
	copy.podInfo = s.podInfo
	copy.namespaceLabels = s.namespaceLabels
	return &copy
}

// updateWithPod updates the preFilterState counters with the (anti)affinity matches for the given podInfo.
func (s *preFilterState) updateWithPod(pInfo *PodInfo, node *v1.Node, multiplier int64) {
	if s == nil {
		return
	}

	s.existingAntiAffinityCounts.updateWithAntiAffinityTerms(pInfo.RequiredAntiAffinityTerms, s.podInfo.Pod, s.namespaceLabels, node, multiplier)
	s.affinityCounts.updateWithAffinityTerms(s.podInfo.RequiredAffinityTerms, pInfo.Pod, node, multiplier)
	// The incoming pod's terms have the namespaceSelector merged into the namespaces, and so
	// here we don't lookup the updated pod's namespace labels, hence passing nil for nsLabels.
	s.antiAffinityCounts.updateWithAntiAffinityTerms(s.podInfo.RequiredAntiAffinityTerms, pInfo.Pod, nil, node, multiplier)
}

type topologyPair struct {
	key   string
	value string
}
type topologyToMatchedTermCount map[topologyPair]int64

func (m topologyToMatchedTermCount) append(toAppend topologyToMatchedTermCount) {
	for pair := range toAppend {
		m[pair] += toAppend[pair]
	}
}

func (m topologyToMatchedTermCount) clone() topologyToMatchedTermCount {
	copy := make(topologyToMatchedTermCount, len(m))
	copy.append(m)
	return copy
}

func (m topologyToMatchedTermCount) update(node *v1.Node, tk string, value int64) {
	if tv, ok := node.Labels[tk]; ok {
		pair := topologyPair{key: tk, value: tv}
		m[pair] += value
		// value could be negative, hence we delete the entry if it is down to zero.
		if m[pair] == 0 {
			delete(m, pair)
		}
	}
}

// updates the topologyToMatchedTermCount map with the specified value
// for each affinity term if "targetPod" matches ALL terms.
func (m topologyToMatchedTermCount) updateWithAffinityTerms(
	terms []AffinityTerm, pod *v1.Pod, node *v1.Node, value int64) {
	if podMatchesAllAffinityTerms(terms, pod) {
		for _, t := range terms {
			m.update(node, t.TopologyKey, value)
		}
	}
}

// updates the topologyToMatchedTermCount map with the specified value
// for each anti-affinity term matched the target pod.
func (m topologyToMatchedTermCount) updateWithAntiAffinityTerms(terms []AffinityTerm, pod *v1.Pod, nsLabels labels.Set, node *v1.Node, value int64) {
	// Check anti-affinity terms.
	for _, t := range terms {
		if t.Matches(pod, nsLabels) {
			m.update(node, t.TopologyKey, value)
		}
	}
}

// returns true IFF the given pod matches all the given terms.
func podMatchesAllAffinityTerms(terms []AffinityTerm, pod *v1.Pod) bool {
	if len(terms) == 0 {
		return false
	}
	for _, t := range terms {
		// The incoming pod NamespaceSelector was merged into the Namespaces set, and so
		// we are not explicitly passing in namespace labels.
		if !t.Matches(pod, nil) {
			return false
		}
	}
	return true
}

// calculates the following for each existing pod on each node:
//  1. Whether it has PodAntiAffinity
//  2. Whether any AffinityTerm matches the incoming pod
func (pr *InterPodAffinityProcessor) getExistingAntiAffinityCounts(ctx context.Context, pod *v1.Pod, nsLabels labels.Set, nodes []*NodeInfo) topologyToMatchedTermCount {
	result := make(topologyToMatchedTermCount)
	for _, nodeInfo := range nodes {
		node := nodeInfo.Node()
		if node == nil {
			klog.ErrorS(nil, "Node not found")
			continue
		}

		for _, existingPod := range nodeInfo.PodsWithRequiredAntiAffinity {
			// this will update the count of topology terms that match against the
			// topology term in the map
			result.updateWithAntiAffinityTerms(existingPod.RequiredAntiAffinityTerms, pod, nsLabels, node, 1)
		}
	}

	return result
}

// finds existing Pods that match affinity terms of the incoming pod's (anti)affinity terms.
// It returns a topologyToMatchedTermCount that are checked later by the affinity
// predicate. With this topologyToMatchedTermCount available, the affinity predicate does not
// need to check all the pods in the cluster.
func (pr *InterPodAffinityProcessor) getIncomingAffinityAntiAffinityCounts(ctx context.Context, podInfo *PodInfo, allNodes []*NodeInfo) (topologyToMatchedTermCount, topologyToMatchedTermCount) {
	affinityCounts := make(topologyToMatchedTermCount)
	antiAffinityCounts := make(topologyToMatchedTermCount)
	if len(podInfo.RequiredAffinityTerms) == 0 && len(podInfo.RequiredAntiAffinityTerms) == 0 {
		return affinityCounts, antiAffinityCounts
	}

	for _, nodeInfo := range allNodes {
		node := nodeInfo.Node()
		if node == nil {
			klog.ErrorS(nil, "Node not found")
			continue
		}
		for _, existingPod := range nodeInfo.Pods {
			affinityCounts.updateWithAffinityTerms(podInfo.RequiredAffinityTerms, existingPod.Pod, node, 1)
			// The incoming pod's terms have the namespaceSelector merged into the namespaces, and so
			// here we don't lookup the existing pod's namespace labels, hence passing nil for nsLabels.
			antiAffinityCounts.updateWithAntiAffinityTerms(podInfo.RequiredAntiAffinityTerms, existingPod.Pod, nil, node, 1)
		}
	}

	return affinityCounts, antiAffinityCounts
}

// PreFilter invoked at the prefilter extension point.
func (pr *InterPodAffinityProcessor) PreFilter(ctx context.Context, pod *v1.Pod) (*preFilterState, error) {
	var allNodes []*NodeInfo
	var nodesWithRequiredAntiAffinityPods []*NodeInfo
	var err error
	if allNodes, err = pr.nodeInfoLister.List(); err != nil {
		return nil, fmt.Errorf("failed to list NodeInfos: %w", err)
	}
	if nodesWithRequiredAntiAffinityPods, err = pr.nodeInfoLister.HavePodsWithRequiredAntiAffinityList(); err != nil {
		return nil, fmt.Errorf("failed to list NodeInfos with pods with affinity: %w", err)
	}

	s := &preFilterState{}

	if s.podInfo, err = NewPodInfo(pod); err != nil {
		return nil, fmt.Errorf("parsing pod: %+v", err)
	}

	for i := range s.podInfo.RequiredAffinityTerms {
		if err := pr.mergeAffinityTermNamespacesIfNotEmpty(&s.podInfo.RequiredAffinityTerms[i]); err != nil {
			return nil, err
		}
	}
	for i := range s.podInfo.RequiredAntiAffinityTerms {
		if err := pr.mergeAffinityTermNamespacesIfNotEmpty(&s.podInfo.RequiredAntiAffinityTerms[i]); err != nil {
			return nil, err
		}
	}
	s.namespaceLabels = GetNamespaceLabelsSnapshot(pod.Namespace, pr.nsLister)

	s.existingAntiAffinityCounts = pr.getExistingAntiAffinityCounts(ctx, pod, s.namespaceLabels, nodesWithRequiredAntiAffinityPods)
	s.affinityCounts, s.antiAffinityCounts = pr.getIncomingAffinityAntiAffinityCounts(ctx, s.podInfo, allNodes)

	return s, nil
}

// Checks if scheduling the pod onto this node would break any anti-affinity
// terms indicated by the existing pods.
func satisfyExistingPodsAntiAffinity(state *preFilterState, nodeInfo *NodeInfo) bool {
	if len(state.existingAntiAffinityCounts) > 0 {
		// Iterate over topology pairs to get any of the pods being affected by
		// the scheduled pod anti-affinity terms
		for topologyKey, topologyValue := range nodeInfo.Node().Labels {
			tp := topologyPair{key: topologyKey, value: topologyValue}
			if state.existingAntiAffinityCounts[tp] > 0 {
				return false
			}
		}
	}
	return true
}

// Checks if the node satisfies the incoming pod's anti-affinity rules.
func satisfyPodAntiAffinity(state *preFilterState, nodeInfo *NodeInfo) bool {
	if len(state.antiAffinityCounts) > 0 {
		for _, term := range state.podInfo.RequiredAntiAffinityTerms {
			if topologyValue, ok := nodeInfo.Node().Labels[term.TopologyKey]; ok {
				tp := topologyPair{key: term.TopologyKey, value: topologyValue}
				if state.antiAffinityCounts[tp] > 0 {
					return false
				}
			}
		}
	}
	return true
}

// Checks if the node satisfies the incoming pod's affinity rules.
func satisfyPodAffinity(state *preFilterState, nodeInfo *NodeInfo) bool {
	podsExist := true
	for _, term := range state.podInfo.RequiredAffinityTerms {
		if topologyValue, ok := nodeInfo.Node().Labels[term.TopologyKey]; ok {
			tp := topologyPair{key: term.TopologyKey, value: topologyValue}
			if state.affinityCounts[tp] <= 0 {
				podsExist = false
			}
		} else {
			// All topology labels must exist on the node.
			return false
		}
	}

	if !podsExist {
		// This pod may be the first pod in a series that have affinity to themselves. In order
		// to not leave such pods in pending state forever, we check that if no other pod
		// in the cluster matches the namespace and selector of this pod, the pod matches
		// its own terms, and the node has all the requested topologies, then we allow the pod
		// to pass the affinity check.
		if len(state.affinityCounts) == 0 && podMatchesAllAffinityTerms(state.podInfo.RequiredAffinityTerms, state.podInfo.Pod) {
			return true
		}
		return false
	}
	return true
}

// Filter invoked at the filter extension point.
// It checks if a pod can be scheduled on the specified node with pod affinity/anti-affinity configuration.
func (pr *InterPodAffinityProcessor) Filter(ctx context.Context, state *preFilterState, pod *v1.Pod, nodeInfo *NodeInfo) error {
	if nodeInfo.Node() == nil {
		return fmt.Errorf("node not found")
	}

	if !satisfyPodAffinity(state, nodeInfo) {
		return fmt.Errorf(ErrReasonAffinityRulesNotMatch)
	}

	if !satisfyPodAntiAffinity(state, nodeInfo) {
		return fmt.Errorf(ErrReasonAntiAffinityRulesNotMatch)
	}

	if !satisfyExistingPodsAntiAffinity(state, nodeInfo) {
		return fmt.Errorf(ErrReasonExistingAntiAffinityRulesNotMatch)
	}

	return nil
}
