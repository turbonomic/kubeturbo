/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
A reasonable amount of this code is carried over from upstream k8s
scheduler plugins (mainly interpodaffinity) and updated/extended to make it
suitable for usage here
*/

package podaffinity

import (
	"context"
	"fmt"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

const (
	// ErrReasonExistingAntiAffinityRulesNotMatch is used for ExistingPodsAntiAffinityRulesNotMatch predicate error.
	ErrReasonExistingAntiAffinityRulesNotMatch = "node(s) didn't satisfy existing pods anti-affinity rules"
	// ErrReasonAffinityRulesNotMatch is used for PodAffinityRulesNotMatch predicate error.
	ErrReasonAffinityRulesNotMatch = "node(s) didn't match pod affinity rules"
	// ErrReasonAntiAffinityRulesNotMatch is used for PodAntiAffinityRulesNotMatch predicate error.
	ErrReasonAntiAffinityRulesNotMatch = "node(s) didn't match pod anti-affinity rules"

	ErrReasonNodeSelectorOrVolumeSelectorNotMatch = "pod or volumes (used by pod) nodeselector rules didn't match the node"
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
	// Indicates that this is a pod from a spread workload associated with topology key as hostname
	isHostnameSpreadOnly bool
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
func (m topologyToMatchedTermCount) updateWithAffinityTerms(srcPodInfo, dstPodInfo *PodInfo,
	node *v1.Node, am *AffinityMapper, podParents map[string]string, value int64) {
	terms := srcPodInfo.RequiredAffinityTerms
	// Is this a flaw in the algorithm? per the rule definition ideally a
	// single rule (out of all listed rules) can also match a destination pod,
	// eg. place a pod in the same zone as pod A and same architecture as pod B.
	// TBD: check if this a flaw upstream also.
	if podMatchesAllAffinityTerms(terms, dstPodInfo.Pod) {
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
		// The incoming pod NamespaceSelector was merged into the Namespaces set on at, and so
		// we are not explicitly passing in namespace labels.
		if !t.Matches(pod, nil) {
			return false
		}
	}
	return true
}

// returns true IFF the given pod matches all the given terms.
func podMatchesAffinityTerm(term AffinityTerm, pod *v1.Pod) bool {
	if !term.Matches(pod, nil) {
		return false
	}
	return true
}

// calculates the following for each existing pod on each node:
//  1. Whether it has PodAntiAffinity
//  2. Whether any AffinityTerm matches the incoming pod
func (pr *PodAffinityProcessor) getExistingAntiAffinityCounts(pod *v1.Pod,
	nsLabels labels.Set, nodes []*NodeInfo) (topologyToMatchedTermCount, bool) {
	result := make(topologyToMatchedTermCount)
	hostnameSpreadOnly := false
	for _, nodeInfo := range nodes {
		node := nodeInfo.Node()
		if node == nil {
			klog.ErrorS(nil, "Node not found")
			continue
		}
		// For creation of affinity mapper maps, "pod" would go in destination workloads
		// and existingPod below will be considered in for src workloads
		for _, existingPod := range nodeInfo.PodsWithRequiredAntiAffinity {
			// this will update the count of topology terms that match against the
			// topology term in the map
			if existingPod.Pod.Name == pod.Name && existingPod.Pod.Namespace == pod.Namespace {
				// we should not be validating against self
				continue
			}
			_, hostnameSpread, termsToConsiderFurther, _ := pr.podAntiAffinityMatchesItsReplica(existingPod, pod, nsLabels)
			if !hostnameSpread {
				termsToConsiderFurther = existingPod.RequiredAntiAffinityTerms
			} else {
				// thisPod will match all its replicas spread across all nodes
				// we will skip considering its matching terms to process placement
				// instead will add this to a spread workload set and create segmentation policy.
				// other replicas will be added to the same set mapped to their parent
				// when its their turn to process their placement across nodes
				if len(termsToConsiderFurther) == 0 {
					// There is no point comparing with other pods as there are no other terms
					hostnameSpreadOnly = true
					break
				}
			}

			if !utilfeature.DefaultFeatureGate.Enabled(features.PeerToPeerAffinityAntiaffinity) {
				// We process the affinityMaps for peer to peer affinity case in getIncomingAffinityAntiAffinityCounts().
				// Because we are processing each pod againt all other pods anyways, it does not make sense
				// to treat incoming and existing pods (on the node under consideration) into 2 different set of rules.
				// That did make sense in the previous algorithm that was simply trying to place each pod on to a given node.
				// In the newer mechanish (PeerToPeerAffinityAntiaffinity) we build source groups wrt to destination groups so
				// going through each pod once (in getIncomingAffinityAntiAffinityCounts()) will suffice. There is no real incoming
				// pod, each pod does exist on one of the nodes.

				// We skip the the older style "result" processing when PeerToPeerAffinityAntiaffinity=true
				// The expectation is that the placement via older algorithm further will be void when result values are not set.
				result.updateWithAntiAffinityTerms(termsToConsiderFurther, pod, nsLabels, node, 1)
			}
		}
	}

	return result, hostnameSpreadOnly
}

func (pr *PodAffinityProcessor) podAntiAffinityMatchesItsReplica(existingPod *PodInfo, thisPod *v1.Pod,
	nsLabels labels.Set) (bool, bool, []AffinityTerm, []AffinityTerm) {
	// Value will be "" if not found in map
	// We ideally should have the parent info available as we build that before
	// we process affinities
	existingPodParent := pr.podToControllerMap[existingPod.Pod.Namespace+"/"+existingPod.Pod.Name]
	thisPodParent := pr.podToControllerMap[thisPod.Namespace+"/"+thisPod.Name]
	sameParent := existingPodParent != "" && thisPodParent != "" &&
		(existingPodParent == thisPodParent)
	if !sameParent {
		return false, false, nil, nil
	}

	nonMatchingTerms := []AffinityTerm{}
	nonSpreadTerms := []AffinityTerm{}
	hostnameSpread := false
	match := false
	for _, t := range existingPod.RequiredAntiAffinityTerms {
		switch {
		case t.Matches(thisPod, nsLabels) && t.TopologyKey == "kubernetes.io/hostname":
			match = true
			hostnameSpread = true
			pr.addPodToHostnameSpreadWorkloads(thisPod.Namespace + "/" + thisPod.Name)
		case t.Matches(thisPod, nsLabels) && t.TopologyKey != "kubernetes.io/hostname":
			match = true
			// In this case we process this pod as rest to get in placement map but
			// will use the label commodity key as parent workload
			// The pods will buy this commodity with parent workload name as key
			// and nodegroups will sell it
			pr.addToOtherSpreadPods(thisPod.Namespace + "/" + thisPod.Name)
			nonMatchingTerms = append(nonMatchingTerms, t)
		default:
			// this caters to all other terms the pod might have
			nonMatchingTerms = append(nonMatchingTerms, t)
			nonSpreadTerms = append(nonSpreadTerms, t)
		}
	}

	return match, hostnameSpread, nonMatchingTerms, nonSpreadTerms
}

func (pr *PodAffinityProcessor) addPodToHostnameSpreadWorkloads(podQualifiedName string) {
	// TODO: consider using a fine grained locks viz one only for this set
	pr.Lock()
	defer pr.Unlock()
	parent := pr.podToControllerMap[podQualifiedName]
	replicas := pr.hostnameSpreadWorkloads[parent]
	if replicas.Len() == 0 { // This takes care of nil value and nil set both
		replicas = sets.NewString(podQualifiedName)
	} else {
		replicas.Insert(podQualifiedName)
	}
	pr.hostnameSpreadWorkloads[parent] = replicas
}

func (pr *PodAffinityProcessor) addToOtherSpreadPods(podQualifiedName string) {
	// TODO: consider using a fine grained locks viz one only for this set
	pr.Lock()
	defer pr.Unlock()
	pr.otherSpreadPods.Insert(podQualifiedName)
}

// finds existing Pods that match affinity terms of the incoming pod's (anti)affinity terms.
// It returns a topologyToMatchedTermCount that are checked later by the affinity
// predicate. With this topologyToMatchedTermCount available, the affinity predicate does not
// need to check all the pods in the cluster.
func (pr *PodAffinityProcessor) getIncomingAffinityAntiAffinityCounts(podInfo *PodInfo,
	allNodes []*NodeInfo) (topologyToMatchedTermCount, topologyToMatchedTermCount, bool) {
	affinityCounts := make(topologyToMatchedTermCount)
	antiAffinityCounts := make(topologyToMatchedTermCount)
	sameParentWithAnotherPod := false
	if len(podInfo.RequiredAffinityTerms) == 0 && len(podInfo.RequiredAntiAffinityTerms) == 0 {
		return affinityCounts, antiAffinityCounts, false
	}

	for _, nodeInfo := range allNodes {
		node := nodeInfo.Node()
		if node == nil {
			klog.ErrorS(nil, "Node not found")
			continue
		}
		for _, existingPod := range nodeInfo.Pods {
			if existingPod.Pod.Name == podInfo.Pod.Name && existingPod.Pod.Namespace == podInfo.Pod.Namespace {
				// we should not be validating against self
				continue
			}
			affinityCounts.updateWithAffinityTerms(podInfo, existingPod, node, pr.affinityMapper, pr.podToControllerMap, 1)
			// The incoming pod's terms have the namespaceSelector merged into the namespaces, and so
			// here we don't lookup the existing pod's namespace labels, hence passing nil for nsLabels.
			// We also skip the terms which match the other replicas emulating a spread workload with hostname as
			// topology key. We create a segmentation policy for hostname spread workloads
			sameParent, hostnameSpread, termsToConsiderFurther, _ := pr.podAntiAffinityMatchesItsReplica(podInfo, existingPod.Pod, nil)
			if sameParent {
				sameParentWithAnotherPod = true
			}
			// TODO: hostnameSpread should be processed regardless of the feature flag
			if !hostnameSpread {
				termsToConsiderFurther = podInfo.RequiredAntiAffinityTerms
			} else if len(termsToConsiderFurther) == 0 {
				// There is no point comparing with other pods as there are no other terms
				// TODO: this might actually be noop as we are already skipping coming here from
				// parent method on a similar check
				break
			}
			antiAffinityCounts.updateWithAntiAffinityTerms(termsToConsiderFurther, existingPod.Pod, nil, node, 1)
		}
	}

	return affinityCounts, antiAffinityCounts, sameParentWithAnotherPod
}

func (pr *PodAffinityProcessor) collectAffinitiesForLogs(podInfo *PodInfo) {
	if glog.V(2) {
		// We don't want to waste time setting the map up if verbosity < 2
		defer pr.Unlock()
		pr.Lock()
		for _, term := range podInfo.ActualAffinityTerms {
			pr.uniqueAffinityTerms.Insert(term.String())
		}
		for _, term := range podInfo.ActualAntiAffinityTerms {
			pr.uniqueAntiAffinityTerms.Insert(term.String())
		}
	}
}

// fill in the map of otherSpreadWorkloads
func (pr *PodAffinityProcessor) collectOtherSpreadWorkloads(podInfo *PodInfo) {
	defer pr.Unlock()
	pr.Lock()
	podQualifiedName := podInfo.Pod.Namespace + "/" + podInfo.Pod.Name
	// TODO: this sort of skips those pods which do not have parents, correct it
	parent := pr.podToControllerMap[podQualifiedName]

	// Initialize a sets.String to store non-hostname anti-term topology keys for this pod
	nonHostnameAntiTermTopologyKeys := sets.NewString()

	for _, antiTerm := range podInfo.RequiredAntiAffinityTerms {
		if antiTerm.TopologyKey == "kubernetes.io/hostname" {
			continue
		}
		if !antiTerm.Matches(podInfo.Pod, nil) {
			continue
		}
		workloads := pr.otherSpreadWorkloads[antiTerm.TopologyKey]
		if workloads.Len() == 0 { // This takes care of nil value and nil set both
			workloads = sets.NewString(parent)
		} else {
			workloads.Insert(parent)
		}
		nonHostnameAntiTermTopologyKeys.Insert(antiTerm.TopologyKey)
		pr.otherSpreadWorkloads[antiTerm.TopologyKey] = workloads
	}

	if nonHostnameAntiTermTopologyKeys.Len() > 0 {
		pr.podNonHostnameAntiTermTopologyKeys[podQualifiedName] = nonHostnameAntiTermTopologyKeys
	}
}

// PreFilter invoked at the prefilter extension point.
func (pr *PodAffinityProcessor) PreFilter(ctx context.Context, pod *v1.Pod) (*preFilterState, error) {
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

	s.existingAntiAffinityCounts, s.isHostnameSpreadOnly = pr.getExistingAntiAffinityCounts(pod, s.namespaceLabels, nodesWithRequiredAntiAffinityPods)
	if s.isHostnameSpreadOnly {
		pr.collectAffinitiesForLogs(s.podInfo)
		// break early
		return s, nil
	}
	sameParentWithAnotherPod := false
	s.affinityCounts, s.antiAffinityCounts, sameParentWithAnotherPod = pr.getIncomingAffinityAntiAffinityCounts(s.podInfo, allNodes)
	// only add spread workloads if there is at least one more replica in the same controller
	if sameParentWithAnotherPod {
		pr.collectOtherSpreadWorkloads(s.podInfo)
	}
	pr.collectAffinitiesForLogs(s.podInfo)
	return s, nil
}

func (pr *PodAffinityProcessor) ProcessAffinityMappings(pod *v1.Pod) error {
	var allNodes []*NodeInfo
	var err error
	if allNodes, err = pr.nodeInfoLister.List(); err != nil {
		return fmt.Errorf("failed to list NodeInfos: %w", err)
	}

	podInfo, err := NewPodInfo(pod)
	if err != nil {

		return fmt.Errorf("parsing pod: %+v", err)
	}

	for i := range podInfo.RequiredAffinityTerms {
		if err := pr.mergeAffinityTermNamespacesIfNotEmpty(&podInfo.RequiredAffinityTerms[i]); err != nil {
			return err
		}
	}
	for i := range podInfo.RequiredAntiAffinityTerms {
		if err := pr.mergeAffinityTermNamespacesIfNotEmpty(&podInfo.RequiredAntiAffinityTerms[i]); err != nil {
			return err
		}
	}

	// This is where the all the building work happens
	sameParentWithAnotherPod := pr.processThisPodWrtAllOthersOnNode(podInfo, allNodes)
	// only add spread workloads if there is at least one more replica in the same controller
	if sameParentWithAnotherPod {
		pr.collectOtherSpreadWorkloads(podInfo)
	}
	pr.collectAffinitiesForLogs(podInfo)
	return nil
}

func (pr *PodAffinityProcessor) processThisPodWrtAllOthersOnNode(podInfo *PodInfo, allNodes []*NodeInfo) bool {
	// true if the parent controller has at least two pods
	sameParentMatchesAntiaffinity := false
	matchAny := false
	for _, nodeInfo := range allNodes {
		node := nodeInfo.Node()
		if node == nil {
			klog.ErrorS(nil, "Node not found")
			continue
		}
		for _, podOnThisNode := range nodeInfo.Pods {
			if podOnThisNode.Pod.Name == podInfo.Pod.Name && podOnThisNode.Pod.Namespace == podInfo.Pod.Namespace {
				// we should not be validating against self
				continue
			}

			// The incoming pod's terms have the namespaceSelector merged into the namespaces, and so
			// here we don't lookup the existing pod's namespace labels, hence passing nil for nsLabels.
			// We also skip the terms which match the other replicas emulating a spread workload with hostname as
			// topology key. We create a segmentation policy for hostname spread workloads
			// We also update maps needed for otherSpreadWorkloads in podAntiAffinityMatchesItsReplica()
			foundMatch, _, _, nonSpreadTerms := pr.podAntiAffinityMatchesItsReplica(podInfo, podOnThisNode.Pod, nil)
			if !foundMatch {
				nonSpreadTerms = podInfo.RequiredAntiAffinityTerms
			} else {
				matchAny = true
				sameParentMatchesAntiaffinity = true
			}
			matchAny = pr.affinityMapper.BuildAffinityMaps(podInfo.RequiredAffinityTerms, podInfo, podOnThisNode, node, Affinity) || matchAny
			pr.affinityMapper.BuildAffinityMaps(nonSpreadTerms, podInfo, podOnThisNode, node, AntiAffinity)
		}
	}
	if !matchAny && podWithRequiredPodAffinity(podInfo.Pod) && !util.PodIsPending(podInfo.Pod) {
		pr.Lock()
		// pods not matching anything but carring an affinity/anti-affinity rule will end up into a reconfigure action
		pr.podsWithNoDestinationMatch.Insert(podInfo.Pod.Namespace + "/" + podInfo.Pod.Name)
		pr.Unlock()
	}
	return sameParentMatchesAntiaffinity
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
		// pod Affinity matches its own labels
		// The matching topology(ies) would be the topology keys in the terms
		if len(state.affinityCounts) == 0 && podMatchesAllAffinityTerms(state.podInfo.RequiredAffinityTerms, state.podInfo.Pod) {
			return true
		}
		return false
	}
	return true
}

// Filter invoked at the filter extension point.
// It checks if a pod can be scheduled on the specified node with pod affinity/anti-affinity configuration.
func (pr *PodAffinityProcessor) Filter(ctx context.Context, state *preFilterState, pod *v1.Pod, nodeInfo *NodeInfo) error {
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

	if !pr.satisfyNodeAffinities(pod, nodeInfo.node) {
		return fmt.Errorf(ErrReasonNodeSelectorOrVolumeSelectorNotMatch)
	}

	return nil
}

func (pr *PodAffinityProcessor) satisfyNodeAffinities(pod *v1.Pod, node *v1.Node) bool {
	// Also honor the nodeAffinity from the PVs of the pod if the pod have the PV attached
	var pvNodeSelectorTerms []v1.NodeSelectorTerm
	if utilfeature.DefaultFeatureGate.Enabled(features.HonorAzLabelPvAffinity) {
		pvNodeSelectorTerms = pr.getAllPvAffinityTerms(pod)
	}

	if matchesNodeAffinity(pod, node) && matchesPvNodeAffinity(pvNodeSelectorTerms, node) {
		return true
	}

	return false
}

func (pr *PodAffinityProcessor) getAllPvAffinityTerms(pod *v1.Pod) []v1.NodeSelectorTerm {
	nodeSelectorTerms := []v1.NodeSelectorTerm{}
	displayName := util.GetPodClusterID(pod)
	mounts := pr.podToVolumesMap[displayName]
	for _, amt := range mounts {
		if amt.UsedVolume != nil && amt.UsedVolume.Spec.NodeAffinity != nil && amt.UsedVolume.Spec.NodeAffinity.Required != nil {
			nodeSelectorTerms = append(nodeSelectorTerms, amt.UsedVolume.Spec.NodeAffinity.Required.NodeSelectorTerms...)
		}
	}
	return nodeSelectorTerms
}

func matchesNodeAffinity(pod *v1.Pod, node *v1.Node) bool {
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
			klog.V(10).Infof("Match for RequiredDuringSchedulingIgnoredDuringExecution node selector terms %+v", nodeSelectorTerms)
			nodeAffinityMatches = nodeAffinityMatches && nodeMatchesNodeSelectorTerms(node, nodeSelectorTerms)
		}
	}
	return nodeAffinityMatches
}

func matchesPvNodeAffinity(pvNodeAffinitySelectorTerms []v1.NodeSelectorTerm, node *v1.Node) bool {
	nodeAffinityMatches := true
	if len(pvNodeAffinitySelectorTerms) > 0 {
		nodeAffinityMatches = nodeMatchesNodeSelectorTerms(node, pvNodeAffinitySelectorTerms)
	}
	return nodeAffinityMatches
}

// nodeMatchesNodeSelectorTerms checks if a node's labels satisfy a list of node selector terms,
// terms are ORed, and an empty list of terms will match nothing.
// TODO: we need to get and process the matchfields section also
func nodeMatchesNodeSelectorTerms(node *v1.Node, nodeSelectorTerms []v1.NodeSelectorTerm) bool {
	for _, req := range nodeSelectorTerms {
		nodeSelector, err := NodeSelectorRequirementsAsSelector(req.MatchExpressions)
		if err != nil {
			klog.V(10).Infof("Failed to parse MatchExpressions: %+v, regarding as not match.", req.MatchExpressions)
			return false
		}
		if nodeSelector.Matches(labels.Set(node.Labels)) {
			return true
		}
	}
	return false
}

// NodeSelectorRequirementsAsSelector converts the []NodeSelectorRequirement api type into a struct that implements
// labels.Selector.
func NodeSelectorRequirementsAsSelector(nsm []v1.NodeSelectorRequirement) (labels.Selector, error) {
	if len(nsm) == 0 {
		return labels.Nothing(), nil
	}
	selector := labels.NewSelector()
	for _, expr := range nsm {
		var op selection.Operator
		switch expr.Operator {
		case v1.NodeSelectorOpIn:
			op = selection.In
		case v1.NodeSelectorOpNotIn:
			op = selection.NotIn
		case v1.NodeSelectorOpExists:
			op = selection.Exists
		case v1.NodeSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		case v1.NodeSelectorOpGt:
			op = selection.GreaterThan
		case v1.NodeSelectorOpLt:
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
