package k8sconntrack

import (
	"errors"
	"sync"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
)

const (
	defaultTransactionCapacity float64 = 50

	zeroTransactionUsed float64 = 0
)

// K8sConntrackMonitor is a resource monitoring worker.
type K8sConntrackMonitor struct {
	config *K8sConntrackMonitorConfig

	k8sConntrackClient *K8sConntrackClient

	nodeList []*api.Node

	nodePodMap map[string][]*api.Pod

	metricSink *metrics.EntityMetricSink

	wg sync.WaitGroup

	stopCh chan struct{}
}

func NewK8sConntrackMonitor(config *K8sConntrackMonitorConfig) (*K8sConntrackMonitor, error) {
	schema := "http"
	if config.enableHttps {
		schema = "https"
	}
	k8sConntrackClientConfig := &K8sConntrackClientConfig{
		schema: schema,
		port:   config.port,
	}
	return &K8sConntrackMonitor{
		config:             config,
		k8sConntrackClient: NewK8sConntrackClient(k8sConntrackClientConfig),
		metricSink:         metrics.NewEntityMetricSink(),
		stopCh:             make(chan struct{}, 1),
	}, nil
}

func (m *K8sConntrackMonitor) reset() {
	m.metricSink = metrics.NewEntityMetricSink()
	m.stopCh = make(chan struct{}, 1)
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.K8sConntrackSource
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) ReceiveTask(task *task.Task) {
	m.reset()

	m.nodeList = task.NodeList()
	m.nodePodMap = util.GroupPodsByNode(task.PodList())
}

func (m *K8sConntrackMonitor) Stop() {
	m.stopCh <- struct{}{}
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) Do() *metrics.EntityMetricSink {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	err := m.RetrieveResourceStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
	}
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.metricSink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *K8sConntrackMonitor) RetrieveResourceStat() error {
	defer func() {
		close(m.stopCh)
	}()

	if m.nodeList == nil || len(m.nodeList) == 0 {
		return errors.New("Invalid nodeList or empty nodeList. Finish Immediately...")
	}
	m.wg.Add(len(m.nodeList))

	for _, node := range m.nodeList {
		go func(n *api.Node) {
			defer m.wg.Done()

			select {
			case <-m.stopCh:
				return
			default:
				m.scrapeK8sConntrack(n)
			}
		}(node)
	}

	m.wg.Wait()

	return nil
}

// Get transaction value from a single host.
func (m *K8sConntrackMonitor) getTransactionFromNode(ip string) ([]Transaction, error) {
	host := Host{
		IP:   ip,
		Port: m.k8sConntrackClient.GetPort(),
	}
	transactions, err := m.k8sConntrackClient.GetTransactionData(host)
	if err != nil {
		return transactions, err
	}
	return transactions, nil
}

// Retrieve resource metrics for the given node.
func (m *K8sConntrackMonitor) scrapeK8sConntrack(node *api.Node) {
	// build pod IP map.
	runningPods, exist := m.nodePodMap[node.Name]
	if !exist {
		glog.Errorf("Failed to get all running pods on node %s.", node.Name)
		return
	}
	podIPMap := m.findPodsIPMapOnNode(runningPods)

	// get IP of the node
	ip, err := util.GetNodeIPForMonitor(node, types.K8sConntrackSource)
	if err != nil {
		glog.Errorf("Failed to get IP address for getting information from K8sConntrack running in node %s", node.Name)
		return
	}

	// get transaction data from node
	transactionData, err := m.getTransactionFromNode(ip)
	if err != nil {
		glog.Errorf("Failed to get transaction data from %s: %s", node.Name, err)
	}

	m.parseTransactionData(runningPods, podIPMap, transactionData)

	glog.V(3).Infof("Finished scrape node %s.", node.Name)
}

// Parse transaction data and create metrics.
func (m *K8sConntrackMonitor) parseTransactionData(pods []*api.Pod, podIPMap map[string]*api.Pod, transactionData []Transaction) {
	runningPodSet := make(map[*api.Pod]struct{})
	for _, p := range pods {
		pod := p
		runningPodSet[pod] = struct{}{}
	}

	for _, transaction := range transactionData {
		for ep, transactionUsedCount := range transaction.EndpointsCounterMap {
			if pod, found := podIPMap[ep]; found {
				glog.V(4).Infof("Transaction count usage of pod %s is %f",
					util.GetPodClusterID(pod), transactionUsedCount)

				// application transaction used
				appTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(metrics.ApplicationType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, transactionUsedCount)

				// application transaction capacity
				appTransactionCapacityCountMetrics := metrics.NewEntityResourceMetric(metrics.ApplicationType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Capacity,
					defaultTransactionCapacity)

				serviceTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(metrics.ServiceType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, transactionUsedCount)

				// service transaction used
				m.metricSink.AddNewMetricEntries(appTransactionUsedCountMetrics,
					appTransactionCapacityCountMetrics,
					serviceTransactionUsedCountMetrics)

				delete(runningPodSet, pod)
			} else {
				glog.Warningf("Don't find pod with IP: %s", ep)
			}
		}
	}

	// TODO for now, if we don't find transaction value for a pod, we assign it to 0.
	for pod := range runningPodSet {
		glog.V(4).Infof("Assign 0 transaction to pod %s", util.GetPodClusterID(pod))
		podTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(metrics.ApplicationType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, zeroTransactionUsed)

		podTransactionCapacityCountMetrics := metrics.NewEntityResourceMetric(metrics.ApplicationType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Capacity,
			defaultTransactionCapacity)

		serviceTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(metrics.ServiceType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, zeroTransactionUsed)

		m.metricSink.AddNewMetricEntries(podTransactionUsedCountMetrics,
			podTransactionCapacityCountMetrics,
			serviceTransactionUsedCountMetrics)

	}
}

// Create a map for the pods running in the given node.
// key: pod IP address; value: pod.
func (m *K8sConntrackMonitor) findPodsIPMapOnNode(runningPods []*api.Pod) map[string]*api.Pod {
	podIPMap := make(map[string]*api.Pod)
	for _, pod := range runningPods {
		podIPMap[pod.Status.PodIP] = pod
	}
	return podIPMap
}
