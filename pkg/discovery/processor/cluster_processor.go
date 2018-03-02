package processor

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"k8s.io/client-go/pkg/api/v1"
	"strings"
)

// Top level object that will connect to the Kubernetes cluster and all the nodes in the cluster.
// It will also query the cluster data from the Kubernetes API server and create the KubeCluster
// entity  to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	clusterInfoScraper cluster.ClusterScraperInterface
	nodeScrapper       kubeclient.KubeHttpClientInterface
	validationResult   *ClusterValidationResult
}

func NewClusterProcessor(kubeClient *cluster.ClusterScraper, kubeletClient *kubeclient.KubeletClient) *ClusterProcessor {
	if kubeClient == nil {
		glog.Errorf("Null kubeclient while creating cluster processor")
		return nil
	}
	if kubeletClient == nil {
		glog.Errorf("Null kubeletclient while creating cluster processor")
		return nil
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: kubeClient,
		nodeScrapper:       kubeletClient,
	}
	return clusterProcessor
}

type ClusterValidationResult struct {
	IsValidated      bool
	UnreachableNodes []string
}

// Connects to the Kubernetes API Server and the nodes in the cluster.
// ClusterProcessor is updated with the validation result.
// Return error only if all the nodes in the cluster are unreachable.
func (processor *ClusterProcessor) ConnectCluster() error {
	if processor.clusterInfoScraper == nil || processor.nodeScrapper == nil {
		return fmt.Errorf("Null kubernetes cluster or node client")
	}

	svcID, err := processor.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}
	glog.V(4).Infof("Created cluster %s\n", svcID)

	// Nodes
	validationResult, err := processor.connectToNodes()
	processor.validationResult = validationResult
	if err != nil {
		return err
	}
	return nil
}

// Connects to all nodes in the cluster.
// Obtains the CPU Frequency of the node to determine the node availability and accessibility.
func (processor *ClusterProcessor) connectToNodes() (*ClusterValidationResult, error) {
	var unreachable []string
	validationResult := &ClusterValidationResult{
		UnreachableNodes: unreachable,
	}

	nodeList, err := processor.clusterInfoScraper.GetAllNodes()
	if err != nil {
		validationResult.IsValidated = false
		return validationResult, fmt.Errorf("Error getting nodes for cluster : %s\n", err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	var connectionErrors []string
	var connectionMsgs []string

	for _, node := range nodeList {
		nodeCpuFrequency, err := checkNode(node, processor.nodeScrapper)
		if err != nil {
			connectionErrors = append(connectionErrors, fmt.Sprintf("%s [err:%s]", node.Name, err))
			unreachable = append(unreachable, node.Name)
		} else {
			connectionMsgs = append(connectionMsgs,
				fmt.Sprintf("%s [cpu:%v MHz]", node.Name, nodeCpuFrequency))
		}
	}
	// collect all connection errors
	errStr := strings.Join(connectionErrors, ", ")

	// All nodes are unreachable
	if len(unreachable) == len(nodeList) {
		glog.Errorf("Errors connecting to nodes : %s\n", errStr)
		validationResult.IsValidated = false
		return validationResult, fmt.Errorf(errStr)
	}

	if len(connectionErrors) > 0 {
		glog.Errorf("Some nodes cannot be reached: %s\n", errStr)
	}
	glog.V(2).Infof("Successfully connected to nodes : %s\n", strings.Join(connectionMsgs, ", "))
	validationResult.IsValidated = true
	return validationResult, nil
}

func checkNode(node *v1.Node, kc kubeclient.KubeHttpClientInterface) (float64, error) {
	ip := repository.ParseNodeIP(node, v1.NodeInternalIP)
	cpuFreq, err := kc.GetMachineCpuFrequency(ip)

	if err != nil {
		return 0.0, err
	}

	nodeCpuFrequency := float64(cpuFreq) / util.MegaToKilo
	return nodeCpuFrequency, nil
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces and set in tge cluster object
func (processor *ClusterProcessor) DiscoverCluster() (*repository.KubeCluster, error) {
	if processor.clusterInfoScraper == nil {
		return nil, fmt.Errorf("Null kubernetes cluster client")
	}
	svcID, err := processor.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}
	kubeCluster := repository.NewKubeCluster(svcID)
	glog.V(2).Infof("Created cluster, clusterId = %s\n", svcID)

	// Discover Nodes
	clusterName := kubeCluster.Name
	nodeList, err := processor.clusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	for _, item := range nodeList {
		nodeEntity := repository.NewKubeNode(item, clusterName)
		kubeCluster.SetNodeEntity(nodeEntity)
	}

	if glog.V(3) {
		kubeCluster.LogClusterNodes()
	}

	// Discover Namespaces and Quotas
	// sum of cluster compute resources
	clusterResources := computeClusterResources(kubeCluster.Nodes)
	if glog.V(2) {
		for rt, res := range clusterResources {
			glog.Infof("cluster resource %s has capacity = %f\n", rt, res.Capacity)
		}
	}

	namespaceProcessor := &NamespaceProcessor{
		ClusterInfoScraper: processor.clusterInfoScraper,
		clusterName:        kubeCluster.Name,
		ClusterResources:   clusterResources,
	}
	kubeCluster.Namespaces, err = namespaceProcessor.ProcessNamespaces()
	if glog.V(4) {
		kubeCluster.LogClusterNamespaces()
	}
	return kubeCluster, nil
}

// Query the Kubernetes API Server and Get the Node objects
func (processor *ClusterProcessor) processNodes(clusterName string) (map[string]*repository.KubeNode, error) {
	nodeList, err := processor.clusterInfoScraper.GetAllNodes()
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
		// Iterate over all compute resource types
		for _, rt := range metrics.KubeComputeResourceTypes {
			// get the compute resource if it exists
			nodeResource, exists := node.ComputeResources[rt]
			if !exists {
				glog.Errorf("Missing %s resource in node %s", rt, node.Name)
				continue
			}
			// add the capacity to the cluster compute resource map
			computeCap, exists := computeResources[rt]
			if !exists {
				computeCap = nodeResource.Capacity
			} else {
				computeCap = computeCap + nodeResource.Capacity
			}
			computeResources[rt] = computeCap
		}
	}

	// create KubeDiscoveredResource object for each compute resource type
	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for _, rt := range metrics.KubeComputeResourceTypes {
		capacity := computeResources[rt]
		r := &repository.KubeDiscoveredResource{
			Type:     rt,
			Capacity: capacity,
		}
		clusterResources[rt] = r
	}
	return clusterResources
}
