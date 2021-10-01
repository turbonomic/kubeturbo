package processor

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	v1 "k8s.io/api/core/v1"
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
	isValidated        bool
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

// Connects to the Kubernetes API Server and the nodes in the cluster.
// ClusterProcessor is updated with the validation result.
// Return error only if all the nodes in the cluster are unreachable.
func (p *ClusterProcessor) ConnectCluster() error {
	if p.clusterInfoScraper == nil || p.nodeScrapper == nil {
		return fmt.Errorf("null kubernetes cluster or node client")
	}
	svcID, err := p.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return fmt.Errorf("cannot obtain service ID for cluster: %s", err)
	}
	glog.V(4).Infof("Obtained kubernetes service ID: %s.", svcID)

	// Nodes
	p.isValidated, err = p.connectToNodes()
	return err
}

// Perform a single node validation
func (p *ClusterProcessor) checkNodesWorker(work chan *v1.Node, done chan bool, index int) {
	glog.V(4).Infof("Node verifier worker %d starting.", index)
	for {
		node, present := <-work
		if !present {
			glog.V(4).Infof("Node verifier worker %d finished. No more work.", index)
			return
		}
		err := checkNode(node, p.nodeScrapper)
		if err != nil {
			glog.Errorf("Failed to verify node %s: %v.", node.Name, err)
		} else {
			// Log the success and send the response to everybody
			glog.V(2).Infof("Successfully verified node %s.", node.Name)
			done <- true
			// Force return here. We are done and notified everybody.
			glog.V(4).Infof("Node verifier worker %d finished. Successful verification.", index)
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

// Connects to at least one node in the cluster.
// Obtains the CPU Frequency of the node to determine the node availability and accessibility.
func (p *ClusterProcessor) connectToNodes() (bool, error) {
	nodeList, err := p.clusterInfoScraper.GetAllNodes()
	if err != nil {
		return false, err
	}
	glog.V(2).Infof("There are %d nodes.", len(nodeList))
	// The connection data
	size := len(nodeList)
	work := make(chan *v1.Node, size)
	done := make(chan bool, size)
	// Create workers
	for i := 0; i < workers; i++ {
		go p.checkNodesWorker(work, done, i)
	}
	// Check
	for _, node := range nodeList {
		work <- node
	}
	close(work)
	// Drain the work queue. The workers will terminate automatically.
	defer drainWorkQueue(work)
	// Results. Wait for no longer than a minute.
	if waitForCompletion(done) {
		glog.V(2).Infof("Successfully connected to at least some nodes.")
		return true, nil
	}
	return false, fmt.Errorf("timeout when connecting to nodes")
}

// Checks the node connectivity be querying the kubelet summary endpoint
func checkNode(node *v1.Node, kc kubeclient.KubeHttpClientInterface) error {
	ip := repository.ParseNodeIP(node, v1.NodeInternalIP)
	_, err := kc.GetSummary(ip, node.Name)
	if err != nil {
		return err
	}
	return nil
}

// Query the Kubernetes API Server to get the cluster nodes and namespaces and set in the cluster object
func (p *ClusterProcessor) DiscoverCluster() (*repository.ClusterSummary, error) {
	if p.clusterInfoScraper == nil {
		return nil, fmt.Errorf("null kubernetes cluster client")
	}
	svcID, err := p.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("failed to obtain service ID for cluster: %v", err)
	}
	glog.V(2).Infof("Obtained kubernetes service ID: %s.", svcID)
	nodeList, err := p.clusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes for cluster %s: %v", svcID, err)
	}
	podList, err := p.clusterInfoScraper.GetAllPods()
	if err != nil {
		return nil, fmt.Errorf("failed to get pods for cluster %s: %v", svcID, err)
	}
	glog.V(2).Infof("Discovering cluster with %d nodes and %d pods.", len(nodeList), len(podList))
	// Create kubeCluster and compute cluster resource
	kubeCluster := repository.NewKubeCluster(svcID, nodeList).WithPods(podList).
		WithMachineSetToNodeUIDsMap(p.clusterInfoScraper.GetMachineSetToNodeUIDsMap(nodeList))

	// Discover Namespaces and Quotas
	NewNamespaceProcessor(p.clusterInfoScraper, kubeCluster).ProcessNamespaces()

	// Discover Workload Controllers
	NewControllerProcessor(p.clusterInfoScraper, kubeCluster).ProcessControllers()

	// Discover Services
	NewServiceProcessor(p.clusterInfoScraper, kubeCluster).ProcessServices()

	// Discover volumes
	NewVolumeProcessor(p.clusterInfoScraper, kubeCluster).ProcessVolumes()

	NewBusinessAppProcessor(p.clusterInfoScraper, kubeCluster).ProcessBusinessApps()

	// Update the pod to controller cache
	if clusterScraper, ok := p.clusterInfoScraper.(*cluster.ClusterScraper); ok {
		clusterScraper.UpdatePodControllerCache(kubeCluster.Pods, kubeCluster.ControllerMap)
	}

	return repository.CreateClusterSummary(kubeCluster), nil
}
