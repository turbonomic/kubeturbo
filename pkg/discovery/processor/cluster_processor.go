package processor

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/golang/glog"
)

// Class to query the cluster data from the Kubernetes API server and create the KubeCluster entity
// to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	ClusterInfoScraper *cluster.ClusterScraper
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) ProcessCluster() (*repository.KubeCluster, error) {
	svcID, err := processor.ClusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}

	kubeCluster := &repository.KubeCluster{
		Name: svcID,
		Nodes: make(map[string]*repository.KubeNode),
		Namespaces: make(map[string]*repository.KubeNamespace),
	}

	// Nodes
	nodes, err := processor.processNodes(kubeCluster.Name)
	if err != nil {
		return nil, fmt.Errorf("%s:%s\n", svcID, err)
	}
	kubeCluster.Nodes = nodes

	// Namespaces and Quotas
	// sum of cluster compute resources
	clusterResources := processor.computeClusterResources(kubeCluster.Nodes)
	for rt, cap := range clusterResources {
		glog.Infof("cluster resource %s has capacity = %f\n", rt, cap)
	}

	namespaceProcessor := &NamespaceProcessor{
		ClusterInfoScraper: processor.ClusterInfoScraper,
		clusterName: kubeCluster.Name,
		ClusterResources: clusterResources,
	}
	kubeCluster.Namespaces, err = namespaceProcessor.ProcessNamespaces()

	return kubeCluster, nil
}

// Query the Kubernetes API Server and Get the Node objects
func (processor *ClusterProcessor) processNodes(clusterName string) (map[string]*repository.KubeNode, error)  {
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.Infof("There are %d nodes\n", len(nodeList))

	nodes := make(map[string]*repository.KubeNode)
	for _, item := range nodeList{
		nodeEntity := repository.NewKubeNode(item, clusterName)
		glog.V(2).Infof("Discovered node entity : %s\n", nodeEntity.KubeEntity.String())
		nodes[item.Name] = nodeEntity
	}

	return nodes, nil
}

func (processor *ClusterProcessor) computeClusterResources(nodes map[string]*repository.KubeNode) (map[metrics.ResourceType]*repository.KubeDiscoveredResource){
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range nodes {
		for rt, resource := range node.ComputeResources {
			if !metrics.IsComputeType(rt) {
				continue
			}
			computeCap, exists := computeResources[rt]
			if !exists {
				computeCap = resource.Capacity
			} else {
				computeCap = computeCap + resource.Capacity
			}
			computeResources[rt] = computeCap
		}
	}

	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for rt, capacity := range computeResources {
		r := &repository.KubeDiscoveredResource{
			Type: rt,
			Capacity: capacity,
		}
		clusterResources[rt] = r
	}
	return clusterResources
}

