package interpodaffinity

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
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

type NamespaceListerImpl struct {
	client            *client.Clientset
	kubeNamespacesMap map[string]*v1.Namespace
}

func NewNamespaceLister(client *client.Clientset, clusterSummary *repository.ClusterSummary) NamespaceLister {
	return &NamespaceListerImpl{
		client:            client,
		kubeNamespacesMap: clusterSummary.KubeNamespacesMap,
	}

}

func (n *NamespaceListerImpl) List(selector labels.Selector) (ret []*v1.Namespace, err error) {
	options := metav1.ListOptions{
		LabelSelector: selector.String(),
	}
	namespaces, err := n.client.CoreV1().Namespaces().List(context.TODO(), options)
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces.Items {
		ret = append(ret, &ns)
	}
	return ret, nil
}

func (n *NamespaceListerImpl) Get(name string) (*v1.Namespace, error) {
	ns, found := n.kubeNamespacesMap[name]
	if !found {
		return nil, fmt.Errorf("namespace %s not found", name)
	}
	return ns, nil
}
