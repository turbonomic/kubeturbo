package processor

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"math/rand"
	"strings"
)

// Class to query the cluster data from the Kubernetes API server and create the KubeCluster entity
// to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	ClusterInfoScraper *cluster.ClusterScraper
	NodeScrapper       *kubeclient.KubeletClient
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces.
func (processor *ClusterProcessor) ConnectCluster(checkAllNodes bool) (*repository.KubeCluster, error) {
	svcID, err := processor.ClusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}
	kubeCluster := repository.NewKubeCluster(svcID)

	// Nodes
	err = processor.ConnectToNodes(checkAllNodes)

	return kubeCluster, err
}

func (processor *ClusterProcessor) ConnectToNodes(checkAllNodes bool) error {
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return fmt.Errorf("Error getting nodes for cluster : %s\n", err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	var connectionErrors []error
	var connectionMsgs []string

	n := rand.Int() % len(nodeList)
	for idx, node := range nodeList {
		if !checkAllNodes && idx != n {
			continue
		}
		ip := repository.ParseNodeIP(node)
		kc := processor.NodeScrapper
		cpuFreq, err := kc.GetMachineCpuFrequency(ip)

		if err != nil {
			connectionErrors = append(connectionErrors, fmt.Errorf("%s:%s", node.Name, err))
		} else {
			nodeCpuFrequency := float64(cpuFreq) / util.MegaToKilo
			connectionMsgs = append(connectionMsgs,
				fmt.Sprintf("%s::%s ---> cpu:%v MHz", node.Name, ip, nodeCpuFrequency))
		}
		if !checkAllNodes && idx == n {
			break
		}
	}
	if len(connectionErrors) == 0 {
		glog.V(2).Infof("Successfully connected to nodes : %s\n", strings.Join(connectionMsgs, ", "))
		return nil
	}
	var errorStr []string
	for i, err := range connectionErrors {
		errorStr = append(errorStr, fmt.Sprintf("Error %d: %s", i, err.Error()))
	}
	errStr := strings.Join(errorStr, "\n")
	glog.V(2).Infof("Errors connecting to nodes : %s\n", errStr)
	return fmt.Errorf(errStr)
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces.
// Creates a KubeCluster entity to represent the cluster and its entities and resources
func (processor *ClusterProcessor) DiscoverCluster(kubeCluster *repository.KubeCluster) error { //(*repository.KubeCluster, error) {
	//svcID, err := processor.ClusterInfoScraper.GetKubernetesServiceID()
	//if err != nil {
	//	return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	//}
	//
	//kubeCluster := &repository.KubeCluster{
	//	Name:       svcID,
	//	Nodes:      make(map[string]*repository.KubeNode),
	//	Namespaces: make(map[string]*repository.KubeNamespace),
	//}
	// Discover Nodes
	clusterName := kubeCluster.Name
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	for _, item := range nodeList {
		kubeNode, exists := kubeCluster.Nodes[item.Name]
		if exists {
			kubeNode.UpdateResources(item)
		} else {
			nodeEntity := repository.NewKubeNode(item, clusterName)
			kubeCluster.Nodes[item.Name] = nodeEntity
		}
	}

	//nodes := make(map[string]*repository.KubeNode)
	//nodes, err := processor.processNodes(kubeCluster.Name)
	//if err != nil {
	//	return fmt.Errorf("%s:%s\n", kubeCluster.Name, err)
	//}
	//kubeCluster.Nodes = nodes
	kubeCluster.LogClusterNodes()

	// Discover Namespaces and Quotas
	// sum of cluster compute resources
	clusterResources := computeClusterResources(kubeCluster.Nodes)
	for rt, cap := range clusterResources {
		glog.V(2).Infof("cluster resource %s has capacity = %f\n", rt, cap.Capacity)
	}

	namespaceProcessor := &NamespaceProcessor{
		ClusterInfoScraper: processor.ClusterInfoScraper,
		clusterName:        kubeCluster.Name,
		ClusterResources:   clusterResources,
	}
	kubeCluster.Namespaces, err = namespaceProcessor.ProcessNamespaces()
	if glog.V(4) {
		kubeCluster.LogClusterNamespaces()
	}
	return nil
}

// Query the Kubernetes API Server and Get the Node objects
func (processor *ClusterProcessor) processNodes(clusterName string) (map[string]*repository.KubeNode, error) {
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	nodes := make(map[string]*repository.KubeNode)
	for _, item := range nodeList {
		nodeEntity := repository.NewKubeNode(item, clusterName)
		nodes[item.Name] = nodeEntity
	}

	return nodes, nil
}

// Sum the compute resource capacities from all the nodes to create the cluster resource capacities
func computeClusterResources(nodes map[string]*repository.KubeNode) map[metrics.ResourceType]*repository.KubeDiscoveredResource {
	// sum the capacities of the node resources
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range nodes {
		for rt, nodeResource := range node.ComputeResources {
			computeCap, exists := computeResources[rt]
			if !exists {
				computeCap = nodeResource.Capacity
			} else {
				computeCap = computeCap + nodeResource.Capacity
			}
			computeResources[rt] = computeCap
		}
	}

	// convert to KubeDiscoveredResource objects
	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for rt, capacity := range computeResources {
		r := &repository.KubeDiscoveredResource{
			Type:     rt,
			Capacity: capacity,
		}
		clusterResources[rt] = r
	}
	return clusterResources
}
