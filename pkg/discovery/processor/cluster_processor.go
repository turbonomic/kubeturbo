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

// Connects to the Kubernetes API Server and the nodes in the cluster.
// If connection is successful a KubeCluster entity to represent the cluster is created
func (processor *ClusterProcessor) ConnectCluster() (*repository.KubeCluster, error) {
	if processor.clusterInfoScraper == nil || processor.nodeScrapper == nil {
		return nil, fmt.Errorf("Null kubernetes cluster or node client")
	}

	svcID, err := processor.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}
	kubeCluster := repository.NewKubeCluster(svcID)
	glog.V(4).Infof("Created cluster %s\n", svcID)

	// Nodes
	err = processor.connectToNodes()
	if err != nil {
		return nil, err
	}

	return kubeCluster, err
}

// Connects to all nodes in the cluster.
// Obtains the CPU Frequency of the node to determine the node availability and accessibility.
func (processor *ClusterProcessor) connectToNodes() error {
	nodeList, err := processor.clusterInfoScraper.GetAllNodes()
	if err != nil {
		return fmt.Errorf("Error getting nodes for cluster : %s\n", err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	var connectionErrors []string
	var connectionMsgs []string

	for _, node := range nodeList {
		nodeCpuFrequency, err := checkNode(node, processor.nodeScrapper)
		if err != nil {
			connectionErrors = append(connectionErrors, fmt.Sprintf("%s:%s", node.Name, err))
		} else {
			connectionMsgs = append(connectionMsgs,
				fmt.Sprintf("%s [cpu:%v MHz]", node.Name, nodeCpuFrequency))
		}
	}

	if len(connectionMsgs) > 0 {
		glog.V(2).Infof("Successfully connected to nodes : %s\n", strings.Join(connectionMsgs, ", "))
	}

	errStr := strings.Join(connectionErrors, ", ")
	if len(connectionErrors) > 0 {
		glog.Errorf("Errors connecting to nodes : %s\n", errStr)
	}

	if len(connectionMsgs) > 0 {
		return nil
	}
	return fmt.Errorf(errStr)
}

func checkNode(node *v1.Node, kc kubeclient.KubeHttpClientInterface) (float64, error) {
	ip := repository.ParseNodeIP(node)
	cpuFreq, err := kc.GetMachineCpuFrequency(ip)

	if err != nil {
		return 0.0, err
	}

	nodeCpuFrequency := float64(cpuFreq) / util.MegaToKilo
	return nodeCpuFrequency, nil
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces and set in tge cluster object
func (processor *ClusterProcessor) DiscoverCluster(kubeCluster *repository.KubeCluster) error {
	// If connection to cluster is successful, then the cluster object is not null
	if kubeCluster == nil {
		return fmt.Errorf("Failed to connect to cluster")
	}
	if processor.clusterInfoScraper == nil {
		return fmt.Errorf("Null kubernetes cluster client")
	}
	// Discover Nodes
	clusterName := kubeCluster.Name
	nodeList, err := processor.clusterInfoScraper.GetAllNodes()
	if err != nil {
		return fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.V(2).Infof("There are %d nodes\n", len(nodeList))

	for _, item := range nodeList {
		kubeNode, err := kubeCluster.GetNodeEntity(item.Name)
		if err == nil {
			kubeNode.UpdateResources(item)
		} else {
			nodeEntity := repository.NewKubeNode(item, clusterName)
			kubeCluster.SetNodeEntity(nodeEntity)
		}
	}

	kubeCluster.LogClusterNodes()

	// Discover Namespaces and Quotas
	// sum of cluster compute resources
	clusterResources := computeClusterResources(kubeCluster.Nodes)
	for rt, cap := range clusterResources {
		glog.V(2).Infof("cluster resource %s has capacity = %f\n", rt, cap.Capacity)
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
	return nil
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
