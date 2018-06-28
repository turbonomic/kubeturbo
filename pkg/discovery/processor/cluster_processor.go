package processor

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"k8s.io/client-go/pkg/api/v1"
)

var (
	workers       = 10
	totalWaitTime = 60 * time.Second
)

// Top level object that will connect to the Kubernetes cluster and all the nodes in the cluster.
// It will also query the cluster data from the Kubernetes API server and create the KubeCluster
// entity  to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	clusterInfoScraper cluster.ClusterScraperInterface
	nodeScrapper       kubeclient.KubeHttpClientInterface
	validationResult   *ClusterValidationResult
}

func NewClusterProcessor(kubeClient *cluster.ClusterScraper, kubeletClient *kubeclient.KubeletClient, ValidationWorkers int,
	ValidationTimeoutSec int) *ClusterProcessor {
	workers = ValidationWorkers
	totalWaitTime = time.Duration(ValidationTimeoutSec) * time.Second
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
		return fmt.Errorf("null kubernetes cluster or node client")
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

// Perform a single node validation
func (processor *ClusterProcessor) checkNodesWorker(work chan *v1.Node, done chan bool, index int) {
	glog.V(2).Infof("node verifier worker %d starting", index)
	for {
		node, present := <-work
		if !present {
			glog.V(2).Infof("node verifier worker %d finished. No more work", index)
			return
		}
		nodeCpuFrequency, err := checkNode(node, processor.nodeScrapper)
		if err != nil {
			glog.Errorf("%s [err:%s]", node.Name, err)
		} else {
			// Log the success and send the response to everybody
			glog.V(2).Infof("verified %s [cpu:%v MHz]", node.Name, nodeCpuFrequency)
			done <- true
			// Force return here. We are done and notified everybody.
			glog.V(2).Infof("node verifier worker %d finished. Successful verification", index)
			return
		}
	}
}

// Wait for at least one of the workers to complete successfully
// or timeout
func waitForCompletion(done chan bool) bool {
	timer := time.NewTimer(totalWaitTime)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// Drains the work queue.
// This will allow us to terminate the still on worker threads
func drainWorkQueue(work chan *v1.Node) {
	for {
		_, ok := <-work
		if !ok {
			return
		}
	}
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
	// The connection data
	size := len(nodeList)
	work := make(chan *v1.Node, size)
	done := make(chan bool, size)
	// Create workers
	for i := 0; i < workers; i++ {
		go processor.checkNodesWorker(work, done, i)
	}
	// Check
	for _, node := range nodeList {
		work <- node
	}
	close(work)
	// Results. Wait for no longer than a minute.
	validationResult.IsValidated = waitForCompletion(done)
	// Drain the work queue. The workers will terminate automatically.
	drainWorkQueue(work)
	// Success, partial or full.
	if validationResult.IsValidated {
		glog.V(2).Infof("Successfully connected to at least some nodes\n")
		return validationResult, nil
	}
	return validationResult, fmt.Errorf("error connecting to any node")
}

// Checks the node connectivity be obtaining its CPU frequency
func checkNode(node *v1.Node, kc kubeclient.KubeHttpClientInterface) (float64, error) {
	ip := repository.ParseNodeIP(node, v1.NodeInternalIP)
	cpuFreq, err := kc.GetMachineCpuFrequency(ip)
	if err != nil {
		return 0.0, err
	}
	return float64(cpuFreq) / util.MegaToKilo, nil
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces and set in tge cluster object
func (processor *ClusterProcessor) DiscoverCluster() (*repository.KubeCluster, error) {
	if processor.clusterInfoScraper == nil {
		return nil, fmt.Errorf("null kubernetes cluster client")
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
		nodeActive := util.NodeIsReady(item) && util.NodeIsSchedulable(item)
		if !nodeActive {
			glog.V(2).Info("Node status is NotReady or NotSchedulable, skip in Quota creation")
		} else {
			nodeEntity := repository.NewKubeNode(item, clusterName)
			kubeCluster.SetNodeEntity(nodeEntity)
		}
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
