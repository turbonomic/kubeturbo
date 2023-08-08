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
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/parallelizer"
)

// PodAffinityProcessor processes inter pod affinities, anti affinities
// and pod to node affinities
type PodAffinityProcessor struct {
	nodeInfoLister          NodeInfoLister
	nsLister                listersv1.NamespaceLister
	podToVolumesMap         map[string][]repository.MountedVolume
	podToControllerMap      map[string]string
	hostnameSpreadWorkloads map[string]sets.String
	otherSpreadPods         sets.String
	uniqueAffinityTerms     sets.String
	uniqueAntiAffinityTerms sets.String
	parallelizer            parallelizer.Parallelizer
	sync.Mutex
}

// New initializes a new plugin and returns it.
func New(clusterSummary *repository.ClusterSummary, nodeInfoLister NodeInfoLister,
	namespaceLister listersv1.NamespaceLister) (*PodAffinityProcessor, error) {
	pr := &PodAffinityProcessor{
		nodeInfoLister:     nodeInfoLister,
		podToVolumesMap:    clusterSummary.PodToVolumesMap,
		podToControllerMap: clusterSummary.PodToControllerMap,
		// TODO: make the parallelizm configurable
		parallelizer:            parallelizer.NewParallelizer(parallelizer.DefaultParallelism),
		nsLister:                namespaceLister,
		uniqueAffinityTerms:     sets.NewString(),
		uniqueAntiAffinityTerms: sets.NewString(),
		hostnameSpreadWorkloads: make(map[string]sets.String),
		otherSpreadPods:         sets.NewString(),
	}

	return pr, nil
}

// Updates Namespaces with the set of namespaces identified by NamespaceSelector.
// If successful, NamespaceSelector is set to nil.
// The assumption is that the term is for an incoming pod, in which case
// namespaceSelector is either unrolled into Namespaces (and so the selector
// is set to Nothing()) or is Empty(), which means match everything. Therefore,
// there when matching against this term, there is no need to lookup the existing
// pod's namespace labels to match them against term's namespaceSelector explicitly.
func (pr *PodAffinityProcessor) mergeAffinityTermNamespacesIfNotEmpty(at *AffinityTerm) error {
	if at.NamespaceSelector.Empty() {
		return nil
	}
	ns, err := pr.nsLister.List(at.NamespaceSelector)
	if err != nil {
		return err
	}
	for _, n := range ns {
		at.Namespaces.Insert(n.Name)
	}
	at.NamespaceSelector = labels.Nothing()
	return nil
}

// GetNamespaceLabelsSnapshot returns a snapshot of the labels associated with
// the namespace.
func GetNamespaceLabelsSnapshot(ns string, nsLister NamespaceLister) (nsLabels labels.Set) {
	podNS, err := nsLister.Get(ns)
	if err == nil {
		// Create and return snapshot of the labels.
		return labels.Merge(podNS.Labels, nil)
	}
	klog.V(3).InfoS("getting namespace, assuming empty set of namespace labels", "namespace", ns, "err", err)
	return
}

// ProcessAffinities returns
// nodesPods: a map of nodes to list of pods which can be placed on each respective node
// podsWithSupportedAffinities:  a set of pods which have affinities (does not include hostnameSpreadWorkloads)
// hostnameSpreadWorkloads: map of workload ids to its set of pods which are spread workloads based on hostname as topology key
// otherSpreadPods: other spread workload pods (those which are based on a topology key that is NOT hostname)
func (pr *PodAffinityProcessor) ProcessAffinities(allPods []*v1.Pod) (map[string][]string,
	sets.String, map[string]sets.String, sets.String) {
	nodeInfos, err := pr.nodeInfoLister.List()
	if err != nil {
		klog.Errorf("Error retreiving nodeinfos while processing affinities, %V.", err)
		return nil, nil, nil, nil
	}

	ctx := context.TODO()
	nodesPods := make(map[string][]string)
	podsWithAllAffinities := sets.NewString()
	// We somehow have never supported the nodeAffinity with matchFields term
	// that is the reason podsWithAllAffinities differs from podsWithSupportedAffinities
	podsWithSupportedAffinities := sets.NewString()
	podsWithVolumeAffinities := sets.NewString()
	podsWithPodAffinities := sets.NewString()
	podsWithNodeAffinities := sets.NewString()
	// We process and log the pod and node labels here as we want this
	// information uniquely relevant to affinities anyways
	podLabels := sets.NewString()
	nodeLabels := sets.NewString()
	if glog.V(3) {
		pr.Lock()
		for _, nodeInfo := range nodeInfos {
			for k, v := range nodeInfo.node.Labels {
				nodeLabels.Insert(k + "=" + v)
			}
		}
		pr.Unlock()
	}

	processAffinityPerPod := func(i int) {
		pod := allPods[i]
		qualifiedPodName := pod.Namespace + "/" + pod.Name
		if !(podWithAffinity(pod) || pr.podsVolumeHasAffinities(qualifiedPodName)) {
			return
		}
		state, err := pr.PreFilter(ctx, pod)
		if err != nil {
			klog.Errorf("Error computing prefilter state for pod, %s.", qualifiedPodName)
			return
		}
		if state.isHostnameSpreadOnly {
			// Skip adding the pod to the placement map
			// We will create a separate segmentation based policy for these
			return
		}

		for _, nodeInfo := range nodeInfos {
			err := pr.Filter(ctx, state, pod, nodeInfo)
			// Err means a placement was not found on this node
			if err != nil {
				continue
			}
			pr.Lock()
			if _, exists := nodesPods[nodeInfo.node.Name]; !exists {
				nodesPods[nodeInfo.node.Name] = []string{}
			}
			nodesPods[nodeInfo.node.Name] = append(nodesPods[nodeInfo.node.Name], qualifiedPodName)
			pr.Unlock()
		}
		pr.Lock()
		podsWithSupportedAffinities.Insert(qualifiedPodName)
		if podWithAffinity(pod) {
			podsWithAllAffinities.Insert(qualifiedPodName)
		}
		if podWithPodAffinityAndAntiaffinity(pod) {
			podsWithPodAffinities.Insert(qualifiedPodName)
		}
		if pr.podsVolumeHasAffinities(qualifiedPodName) {
			podsWithVolumeAffinities.Insert(qualifiedPodName)
		}
		if podWithNodeAffinity(pod) {
			podsWithNodeAffinities.Insert(qualifiedPodName)
		}
		if glog.V(3) {
			for k, v := range pod.Labels {
				podLabels.Insert(k + "=" + v)
			}
		}
		pr.Unlock()
	}

	pr.parallelizer.Until(context.Background(), len(allPods), processAffinityPerPod, "processAffinityPerPod")

	if glog.V(3) {
		glog.Infof("Cluster has %v total pods with All Affinities/AntiAffinities.", podsWithAllAffinities.Len())
		glog.Infof("Cluster has %v total pods with node Affinities/AntiAffinities.", podsWithNodeAffinities.Len())
		glog.Infof("Cluster has %v pods with pod to pod Affinities/AntiAffinities.", podsWithPodAffinities.Len())
		glog.Infof("Cluster has %v pods with volumes that specify Node Affinities/AntiAffinities.", podsWithVolumeAffinities.Len())
		glog.Infof("Cluster has %v total unique Affinity terms by rule string.", pr.uniqueAffinityTerms.Len())
		glog.Infof("Cluster has %v total unique AntiAffinity terms by rule string.", pr.uniqueAntiAffinityTerms.Len())
		glog.Infof("Cluster has %v total unique label key=value pairs on pods.", podLabels.Len())
		glog.Infof("Cluster has %v total unique label key=value pairs (topologies) on nodes.", nodeLabels.Len())
		spreadPodCount := 0
		for _, w := range pr.hostnameSpreadWorkloads {
			spreadPodCount += w.Len()
		}
		glog.Infof("Cluster has %v spread workloads wrt node hostnames with overall total of %v pods.", len(pr.hostnameSpreadWorkloads), spreadPodCount)
		glog.Infof("Cluster has total of %v spread workload pods wrt other topology keys.", pr.otherSpreadPods.Len())
	}
	if glog.V(5) {
		glog.Infof("Pods with Affinities/AntiAffinities: \n%v\n", podsWithAllAffinities.UnsortedList())
		glog.Infof("Pods with node Affinities/AntiAffinities: \n%v\n", podsWithNodeAffinities.UnsortedList())
		glog.Infof("Pods with pod to pod Affinities/AntiAffinities: \n%v\n", podsWithPodAffinities.UnsortedList())
		glog.Infof("Pods with volumes that specify Node Affinities/AntiAffinities: \n%v\n", podsWithVolumeAffinities.UnsortedList())
		glog.Infof("Pods with spread workloads wrt hostnames: \n%v\n", pr.hostnameSpreadWorkloads)
		glog.Infof("Pods with spread workloads but not wrt hostnames: \n%v\n", pr.otherSpreadPods)
		glog.Infof("Unique Affinity terms by rule string: \n%v\n", pr.uniqueAffinityTerms.UnsortedList())
		glog.Infof("Unique AntiAffinity terms by rule string: \n%v\n", pr.uniqueAntiAffinityTerms.UnsortedList())
		glog.Infof("Unique label pairs on pods: \n %v\n", podLabels.UnsortedList())
		glog.Infof("Unique label pairs (topologies) on nodes: \n %v\n", nodeLabels.UnsortedList())
	}

	return nodesPods, podsWithSupportedAffinities, pr.hostnameSpreadWorkloads, pr.otherSpreadPods
}

func (pr *PodAffinityProcessor) podHasVolumes(qualifiedPodName string) bool {
	_, exists := pr.podToVolumesMap[qualifiedPodName]
	return exists
}

func (pr *PodAffinityProcessor) podsVolumeHasAffinities(qualifiedPodName string) bool {
	vols, exists := pr.podToVolumesMap[qualifiedPodName]
	if !exists {
		return false
	}
	for _, vol := range vols {
		if vol.UsedVolume != nil && vol.UsedVolume.Spec.NodeAffinity != nil &&
			vol.UsedVolume.Spec.NodeAffinity.Required != nil &&
			len(vol.UsedVolume.Spec.NodeAffinity.Required.NodeSelectorTerms) > 0 {
			return true
		}
	}
	return false
}
