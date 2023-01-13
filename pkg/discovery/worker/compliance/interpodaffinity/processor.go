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

package interpodaffinity

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

// InterPodAffinityProcessor processes inter pod affinities
type InterPodAffinityProcessor struct {
	nodeInfoLister NodeInfoLister
	nsLister       NamespaceLister
}

// New initializes a new plugin and returns it.
func New(client *client.Clientset, clusterSummary *repository.ClusterSummary) (*InterPodAffinityProcessor, error) {
	pr := &InterPodAffinityProcessor{
		nodeInfoLister: NewNodeInfoLister(clusterSummary),
		nsLister:       NewNamespaceLister(client, clusterSummary),
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
func (pr *InterPodAffinityProcessor) mergeAffinityTermNamespacesIfNotEmpty(at *AffinityTerm) error {
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

func (pr *InterPodAffinityProcessor) ProcessAffinities(allPods []*v1.Pod) map[string][]string {
	result := make(map[string][]string)
	ctx := context.TODO()

	nodeInfos, err := pr.nodeInfoLister.List()
	if err != nil {
		klog.Errorf("Error retreiving nodeinfos while processing affinities, %V.", err)
		return nil
	}

	for _, pod := range allPods {
		state, err := pr.PreFilter(ctx, pod)
		if err != nil {
			klog.Errorf("Error computing prefilter state for pod, %s/%s.", pod.Namespace, pod.Name)
			continue
		}

		nodeList := []string{}
		result[pod.Name] = nodeList
		for _, nodeInfo := range nodeInfos {
			err := pr.Filter(ctx, state, pod, nodeInfo)
			if err != nil {
				continue
			}
			result[pod.Name] = append(result[pod.Name], nodeInfo.node.Name)
		}
	}

	return result
}
