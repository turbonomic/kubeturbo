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
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	client "k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/parallelizer"
)

// PodAffinityProcessor processes inter pod affinities, anti affinities
// and pod to node affinities
type PodAffinityProcessor struct {
	nodeInfoLister  NodeInfoLister
	nsLister        listersv1.NamespaceLister
	podToVolumesMap map[string][]repository.MountedVolume
	parallelizer    parallelizer.Parallelizer
	sync.Mutex
}

// New initializes a new plugin and returns it.
func New(client *client.Clientset, clusterSummary *repository.ClusterSummary) (*PodAffinityProcessor, error) {
	pr := &PodAffinityProcessor{
		nodeInfoLister:  NewNodeInfoLister(clusterSummary),
		podToVolumesMap: clusterSummary.PodToVolumesMap,
		// TODO: make the parallelizm configurable
		parallelizer: parallelizer.NewParallelizer(parallelizer.DefaultParallelism),
	}
	nsLister, err := NewNamespaceLister(client, clusterSummary)
	if err != nil {
		return nil, fmt.Errorf("error creating affinity processor: %v", err)
	}

	pr.nsLister = nsLister
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

// ProcessAffinities returns a map of nodes to list of pods which can be placed on each
// respective node and a set of pods which have affinities.
func (pr *PodAffinityProcessor) ProcessAffinities(allPods []*v1.Pod) (map[string][]string, sets.String) {
	nodeInfos, err := pr.nodeInfoLister.List()
	if err != nil {
		klog.Errorf("Error retreiving nodeinfos while processing affinities, %V.", err)
		return nil, nil
	}

	podsWithAffinities := sets.NewString()
	nodesPods := make(map[string][]string)
	ctx := context.TODO()
	processAffinityPerPod := func(i int) {
		pod := allPods[i]
		qualifiedPodName := pod.Namespace + "/" + pod.Name
		if !(podWithAffinity(pod) || pr.podHasVolumes(qualifiedPodName)) {
			return
		}
		state, err := pr.PreFilter(ctx, pod)
		if err != nil {
			klog.Errorf("Error computing prefilter state for pod, %s.", qualifiedPodName)
			return
		}

		placed := false
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
			placed = true
			pr.Unlock()
		}
		pr.Lock()
		if placed {
			podsWithAffinities.Insert(qualifiedPodName)
		}
		pr.Unlock()
	}

	pr.parallelizer.Until(context.Background(), len(allPods), processAffinityPerPod, "processAffinityPerPod")
	return nodesPods, podsWithAffinities
}

func (pr *PodAffinityProcessor) podHasVolumes(qualifiedPodName string) bool {
	_, exists := pr.podToVolumesMap[qualifiedPodName]
	return exists
}
