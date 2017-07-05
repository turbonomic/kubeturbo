package k8sconntrack

import (
	"errors"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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

	metricSink *metrics.EntityMetricSink

	wg sync.WaitGroup
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
	}, nil
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.K8sConntrackSource
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) ReceiveTask(task *task.Task) {
	m.nodeList = task.NodeList()
}

// Implement MonitoringWorker interface.
func (m *K8sConntrackMonitor) Do() *metrics.EntityMetricSink {
	err := m.RetrieveResourceStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
	}

	return m.metricSink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *K8sConntrackMonitor) RetrieveResourceStat() error {
	if m.nodeList == nil || len(m.nodeList) == 0 {
		return errors.New("Invalid nodeList or empty nodeList. Finish Immediately...")
	}
	m.wg.Add(len(m.nodeList))

	for _, node := range m.nodeList {

		go m.scrapeK8sConntrack(node)
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
	defer m.wg.Done()

	// build pod IP map.
	runningPods, err := m.findRunningPodOnNode(node.Name)
	if err != nil {
		glog.Errorf("Failed to get all running pods on node %s: %s", node.Name, err)
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
func (m *K8sConntrackMonitor) parseTransactionData(runningPods map[*api.Pod]struct{}, podIPMap map[string]*api.Pod, transactionData []Transaction) {
	for _, transaction := range transactionData {
		for ep, transactionUsedCount := range transaction.EndpointsCounterMap {
			if pod, found := podIPMap[ep]; found {
				glog.V(4).Infof("Transaction count usage of pod %s is %f",
					util.GetPodClusterID(pod), transactionUsedCount)

				// application transaction used
				appTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, transactionUsedCount)

				// application transaction capacity
				appTransactionCapacityCountMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Capacity,
					defaultTransactionCapacity)

				serviceTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(task.ServiceType,
					util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, transactionUsedCount)

				// service transaction used
				m.metricSink.AddNewMetricEntries(appTransactionUsedCountMetrics,
					appTransactionCapacityCountMetrics,
					serviceTransactionUsedCountMetrics)

				delete(runningPods, pod)
			} else {
				glog.Warningf("Don't find pod with IP: %s", ep)
			}
		}
	}

	// TODO for now, if we don't find transaction value for a pod, we assign it to 0.
	for pod := range runningPods {
		glog.V(4).Infof("Assign 0 transaction to pod %s", util.GetPodClusterID(pod))
		podTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, zeroTransactionUsed)

		podTransactionCapacityCountMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Capacity,
			defaultTransactionCapacity)

		serviceTransactionUsedCountMetrics := metrics.NewEntityResourceMetric(task.ServiceType,
			util.PodKeyFunc(pod), metrics.Transaction, metrics.Used, zeroTransactionUsed)

		m.metricSink.AddNewMetricEntries(podTransactionUsedCountMetrics,
			podTransactionCapacityCountMetrics,
			serviceTransactionUsedCountMetrics)

	}
}

func (m *K8sConntrackMonitor) findRunningPodOnNode(nodeName string) (map[*api.Pod]struct{}, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase=" +
		string(api.PodRunning))
	if err != nil {
		return nil, err
	}
	podList, err := m.config.kubeClient.CoreV1().Pods(api.NamespaceAll).List(metav1.ListOptions{FieldSelector: fieldSelector.String()})
	if err != nil {
		return nil, err
	}
	runningPods := make(map[*api.Pod]struct{})
	for _, p := range podList.Items {
		pod := p
		runningPods[&pod] = struct{}{}
	}
	return runningPods, nil
}

// Create a map for the pods running in the given node.
// key: pod IP address; value: pod.
func (m *K8sConntrackMonitor) findPodsIPMapOnNode(runningPods map[*api.Pod]struct{}) map[string]*api.Pod {
	podIPMap := make(map[string]*api.Pod)
	for pod := range runningPods {
		podIPMap[pod.Status.PodIP] = pod
	}
	return podIPMap
}
