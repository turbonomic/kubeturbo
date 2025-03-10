package processor

import (
	"fmt"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/util/feature"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	"github.ibm.com/turbonomic/kubeturbo/pkg/kubeclient"
)

const (
	// DefaultItemsPerGiMemory defines number of items to retrieve for each GB of memory
	DefaultItemsPerGiMemory                     = 5000
	DefaultAutoMemLimitPct                      = 0.9
	DefaultGenericMetricSizePerThousandPodsInGi = 0.24
	DefaultCadvisorMetricSizePerPodInGi         = 0.004
	DefaultMaxPodsPerNode                       = 250
	DefaultExtraPerNodeUsageInGi                = 0.2
	DefaultExtraClusterWideUsageInGi            = 0.2
)

var (
	workers       = 10
	totalWaitTime = 60 * time.Second
)

// ClusterProcessor defines top level object that will connect to the Kubernetes cluster and all the
// nodes in the cluster.
// It will also query the cluster data from the Kubernetes API server and create the KubeCluster
// entity  to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	clusterInfoScraper cluster.ClusterScraperInterface
	nodeScrapper       kubeclient.KubeHttpClientInterface
	isValidated        bool
	itemsPerListQuery  int
	isOpenShift        bool
}

func NewClusterProcessor(
	kubeClient *cluster.ClusterScraper, kubeletClient *kubeclient.KubeletClient,
	ValidationWorkers, ValidationTimeoutSec, itemsPerListQuery int, isOpenShift bool) *ClusterProcessor {
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
		itemsPerListQuery:  itemsPerListQuery,
		isOpenShift:        isOpenShift,
	}
	return clusterProcessor
}

// ConnectCluster connects to the Kubernetes API Server and the nodes in the cluster.
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
	nodeList, err := p.GetAllNodes()
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

func (p *ClusterProcessor) GetAllNodes() ([]*v1.Node, error) {
	return p.clusterInfoScraper.GetAllNodes()
}

// DiscoverCluster queries the Kubernetes API Server to get the cluster nodes and namespaces
// and set in the cluster object
func (p *ClusterProcessor) DiscoverCluster() (*repository.ClusterSummary, error) {
	if p.clusterInfoScraper == nil {
		return nil, fmt.Errorf("null kubernetes cluster client")
	}
	svcID, err := p.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("failed to obtain service ID for cluster: %v", err)
	}
	glog.V(2).Infof("Obtained kubernetes service ID: %s.", svcID)
	nodeList, err := p.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes for cluster %s: %v", svcID, err)
	}
	podList, err := p.clusterInfoScraper.GetAllPods()
	if err != nil {
		return nil, fmt.Errorf("failed to get pods for cluster %s: %v", svcID, err)
	}
	podCount := len(podList)
	glog.V(2).Infof("Discovering cluster with %d nodes and %d pods.", len(nodeList), podCount)
	itemsPerListQuery := p.itemsPerListQuery
	if feature.DefaultFeatureGate.Enabled(features.GoMemLimit) && itemsPerListQuery == 0 {
		// Determine items per list API call
		items, limit, err := p.calculateItemsPerListQuery(podCount)
		if err != nil {
			itemsPerListQuery = DefaultItemsPerGiMemory
			glog.Warningf("Cannot calculate items per list API call: %v.", err)
			glog.V(2).Infof("Set items per list API call to the default value of %v.", itemsPerListQuery)
		} else {
			itemsPerListQuery = items
			glog.V(2).Infof("Set items per list API call to %v based on memory limit of %.3f Gi "+
				"and pod count of %v.", items, limit, podCount)
		}
	}
	// Create kubeCluster and compute cluster resource
	kubeCluster := repository.NewKubeCluster(svcID, nodeList).WithPods(podList).
		WithMachineSetToNodesMap(p.clusterInfoScraper.GetMachineSetToNodesMap(nodeList, p.isOpenShift))

	// Discover Namespaces and Quotas
	kubeCluster.KubeNamespacesMap = NewNamespaceProcessor(p.clusterInfoScraper, kubeCluster).ProcessNamespaces()

	// Discover Workload Controllers
	NewControllerProcessor(p.clusterInfoScraper, kubeCluster).
		WithItemsPerListQuery(itemsPerListQuery).
		ProcessControllers()

	// Discover Services
	NewServiceProcessor(p.clusterInfoScraper, kubeCluster).ProcessServices()

	// Discover Volumes
	NewVolumeProcessor(p.clusterInfoScraper, kubeCluster).ProcessVolumes()

	// Discover Business Apps
	NewBusinessAppProcessor(p.clusterInfoScraper, kubeCluster).ProcessBusinessApps()

	// Discover Turbo Policies
	NewTurboPolicyProcessor(p.clusterInfoScraper, kubeCluster).ProcessTurboPolicies()

	// Update the pod to controller cache
	if clusterScraper, ok := p.clusterInfoScraper.(*cluster.ClusterScraper); ok {
		podToControllerMap := clusterScraper.UpdatePodControllerCache(kubeCluster.Pods, kubeCluster.ControllerMap)
		kubeCluster.WithPodToControllerMap(podToControllerMap)
	}

	// Discover and cache GitOps configuration overrides
	p.clusterInfoScraper.UpdateGitOpsConfigCache()

	return repository.CreateClusterSummary(kubeCluster), nil
}

// calculateItemsPerListQuery dynamically calculates the number of items per query to avoid OOM.
// This value must be calculated dynamically because:
//   - the number of pods changes over time
//   - the kubeturbo memory limit can change over time (when in-place pod resize is enabled)
//   - gb here refers to Gi(1Gi = 1024*1024*1024)
//
// The following formula is used:
//
// (kubeturbo_mem_limit_gb * default_automemlimit
//   - generic_metric_size_gb_per_pod * number_of_pods_in_thousands_in_cluster
//   - (cadvisor_metric_size_gb_per_pod * max_pods_per_node + extra_per_node_usage_gb)
//   - extra_cluster_wide_usage_gb) * items_per_gb
//
// where the following values are set based on kubeturbo memory profiling:
//   - default_automemlimit = 0.9 (percentage of memory limit that can be controlled by Go Runtime)
//   - generic_metric_size_gb_per_pod = 0.24
//   - cadvisor_metric_size_gb_per_pod = 0.004
//   - max_pods_per_node = 250
//   - extra_per_node_usage_gb = 0.2
//   - extra_cluster_wide_usage_gb = 0.2
//   - items_per_gb = 5000
//
// which equals to:
//
//	(kubeturbo_mem_limit_gb * 0.9 - number_of_pods_in_thousands_in_cluster * 0.24 - 1.4) *  5000
func (p *ClusterProcessor) calculateItemsPerListQuery(podCount int) (int, float64, error) {
	limit, err := memlimit.FromCgroup()
	if err != nil {
		// This is very unlikely because in absense of any limit set in container resources
		// we will get the cgroup limit as the nodes available/usable memory limit
		return 0, 0, fmt.Errorf("error retrieving memory limit (%v): %v", limit, err)
	}
	if limit == 0 {
		// This is very unlikely because in absense of any limit set in container resources
		// we will get the cgroup limit as the nodes available/usable memory limit
		return 0, 0, fmt.Errorf("limit found set to zero (0)")
	}
	podsInThousands := float64(podCount) / 1000
	//round up recommended limit by 1 MB increment value
	incrementRecommendInGi := 0.001
	currentLimitInGi := util.Base2BytesToGigabytes(float64(limit))
	availMemInGi := currentLimitInGi*DefaultAutoMemLimitPct -
		DefaultGenericMetricSizePerThousandPodsInGi*podsInThousands -
		(DefaultCadvisorMetricSizePerPodInGi*DefaultMaxPodsPerNode + DefaultExtraPerNodeUsageInGi) -
		DefaultExtraClusterWideUsageInGi
	if availMemInGi < 1.0 {
		recommendedLimitInGi := (1.0 + DefaultExtraClusterWideUsageInGi +
			(DefaultCadvisorMetricSizePerPodInGi*DefaultMaxPodsPerNode + DefaultExtraPerNodeUsageInGi) +
			DefaultGenericMetricSizePerThousandPodsInGi*podsInThousands) / DefaultAutoMemLimitPct
		return 0, 0, fmt.Errorf("the memory limit of kubeturbo %.3f Gi is too low to calculate "+
			"a reasonable number of items per list API call. Kubeturbo may run into OOM. "+
			"Consider increasing the memory limit of kubeturbo to at least %.3f Gi",
			currentLimitInGi, recommendedLimitInGi+incrementRecommendInGi)
	}
	return int(DefaultItemsPerGiMemory * availMemInGi), currentLimitInGi, nil
}
