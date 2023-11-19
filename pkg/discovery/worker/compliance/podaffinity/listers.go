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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	client "k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

// NodeInfoLister interface represents anything that can list/get NodeInfo objects from node name.
type NodeInfoLister interface {
	// List returns the list of NodeInfos.
	List() ([]*NodeInfo, error)
	// HavePodsWithRequiredAntiAffinityList returns the list of NodeInfos of nodes with pods with required anti-affinity terms.
	HavePodsWithRequiredAntiAffinityList() ([]*NodeInfo, error)
}

// NamespaceLister helps list Namespaces.
// All objects returned here must be treated as read-only.
type NamespaceLister interface {
	// List lists all Namespaces in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.Namespace, err error)
	// Get retrieves the Namespace from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.Namespace, error)
}

type NodeInfoListerImpl struct {
	nodeInfos                     []*NodeInfo
	nodesWithRequiredAntiAffinity []*NodeInfo
}

func NewNodeInfoLister(clusterSummary *repository.ClusterSummary) NodeInfoLister {
	lister := &NodeInfoListerImpl{}
	for _, node := range clusterSummary.Nodes {
		allPods := []*v1.Pod{}
		allPods = append(allPods, clusterSummary.NodeToRunningPods[node.Name]...)
		allPods = append(allPods, clusterSummary.NodeToPendingPods[node.Name]...)
		nodeInfo := NewNodeInfo(allPods...)
		nodeInfo.SetNode(node)

		lister.nodeInfos = append(lister.nodeInfos, nodeInfo)
		if len(nodeInfo.PodsWithRequiredAntiAffinity) > 0 {
			lister.nodesWithRequiredAntiAffinity = append(lister.nodesWithRequiredAntiAffinity, nodeInfo)
		}
	}

	return lister
}

func (n *NodeInfoListerImpl) List() ([]*NodeInfo, error) {
	return n.nodeInfos, nil
}

func (n *NodeInfoListerImpl) HavePodsWithRequiredAntiAffinityList() ([]*NodeInfo, error) {
	return n.nodesWithRequiredAntiAffinity, nil
}

func NewNamespaceLister(client *client.Clientset, clusterSummary *repository.ClusterSummary) (listersv1.NamespaceLister, error) {
	factory := informers.NewSharedInformerFactory(client, 0)
	nsInformer := factory.Core().V1().Namespaces()
	informer := nsInformer.Informer()
	stopCh := make(chan struct{})
	factory.Start(stopCh) // runs in background
	//factory.WaitForCacheSync(stopCh)

	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {

		//	return
		// TODO
		return nil, fmt.Errorf("failed to sync lister cache while processing podaffinities")
	}

	return nsInformer.Lister(), nil

}
