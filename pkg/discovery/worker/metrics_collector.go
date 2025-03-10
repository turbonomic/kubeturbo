package worker

import (
	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

// MetricsCollector collects allocation metrics for quotas, nodes and pods using the compute resource usages for pods
type MetricsCollector struct {
	Node        *v1.Node
	PodList     []*v1.Pod
	MetricsSink *metrics.EntityMetricSink
	Cluster     *repository.ClusterSummary
	workerId    string
}

func NewMetricsCollector(discoveryWorker *k8sDiscoveryWorker, currTask *task.Task) *MetricsCollector {
	metricsCollector := &MetricsCollector{
		Node:        currTask.Node(),
		PodList:     currTask.PodList(),
		Cluster:     currTask.Cluster(),
		MetricsSink: discoveryWorker.sink,
		workerId:    discoveryWorker.id,
	}
	return metricsCollector
}

// PodMetricsList is the abstraction for a list of PodMetrics
type PodMetricsList []*repository.PodMetrics
type PodMetricsByNodeAndNamespace map[string]map[string]PodMetricsList

func (podMetricsList PodMetricsList) getPodNames() string {
	podNames := ""
	for _, pod := range podMetricsList {
		if pod != nil {
			podNames = podNames + "," + pod.PodName
		}
	}
	return podNames
}

// SumQuotaUsage returns the sum of quota resources usage for all the pods in the collection
func (podMetricsList PodMetricsList) SumQuotaUsage() map[metrics.ResourceType]float64 {
	quotaResourcesSum := make(map[metrics.ResourceType]float64)
	// Sum quota resources usage from pods for each quota type
	for _, quotaType := range metrics.QuotaResources {
		var totalQuotaUsed float64
		for _, podMetrics := range podMetricsList {
			if podMetrics.QuotaUsed == nil {
				continue
			}
			quotaUsed, exists := podMetrics.QuotaUsed[quotaType]
			if !exists {
				glog.Errorf("Cannot find corresponding quota type for %s", quotaType)
				continue
			}
			totalQuotaUsed += quotaUsed
		}
		quotaResourcesSum[quotaType] = totalQuotaUsed
	}
	glog.V(4).Infof("Collected quota resources for pod collection %s",
		podMetricsList.getPodNames())
	for rt, used := range quotaResourcesSum {
		glog.V(4).Infof("\t type=%s used=%f", rt, used)
	}
	return quotaResourcesSum
}

// SumUsage returns the sum of usage for all the pods in the collection
func (podMetricsList PodMetricsList) SumUsage() map[metrics.ResourceType][]metrics.Point {
	resourcesUsedSum := make(map[metrics.ResourceType][]metrics.Point)
	for _, podMetrics := range podMetricsList {
		util.AccumulateMultiPoints(resourcesUsedSum, podMetrics.Used)
	}
	return resourcesUsedSum
}

func (podCollectionMap PodMetricsByNodeAndNamespace) addPodMetric(podName, nodeName, namespace string,
	podMetrics *repository.PodMetrics) {
	podsByNamespaceMap, exists := podCollectionMap[nodeName]
	if !exists {
		// create the map for this node
		podCollectionMap[nodeName] = make(map[string]PodMetricsList)
	}
	podsByNamespaceMap, _ = podCollectionMap[nodeName]

	_, exists = podsByNamespaceMap[namespace]
	if !exists {
		// create the map for this quota
		podsByNamespaceMap[namespace] = PodMetricsList{}
	}
	podMetricsList, _ := podsByNamespaceMap[namespace]
	podMetricsList = append(podMetricsList, podMetrics)
	podsByNamespaceMap[namespace] = podMetricsList
	podCollectionMap[nodeName] = podsByNamespaceMap
	glog.V(4).Infof("Created pod metrics for %s, namespace=%s, node=%s", podName, namespace, nodeName)
}

// -------------------------------------------------------------------------------------------------

// CollectPodMetrics creates Pod metrics by selecting the pod compute resource usages from the metrics sink.
// The PodMetrics are organized in a map by node and namespace.
// The discovery worker will add the PodMetrics to the metrics sink.
func (collector *MetricsCollector) CollectPodMetrics() PodMetricsByNodeAndNamespace {
	if collector.Cluster == nil {
		glog.Errorf("Cluster summary object is null for discovery worker %s", collector.workerId)
		return nil
	}
	podCollectionMap := make(PodMetricsByNodeAndNamespace)
	// Iterate over all pods
	for _, pod := range collector.PodList {
		// Find namespace entity for the pod if available
		kubeNamespace := collector.Cluster.GetKubeNamespace(pod.ObjectMeta.Namespace)
		if kubeNamespace == nil {
			continue
		}
		// quota metrics for the pod
		podMetrics := createPodMetrics(pod, kubeNamespace.Name, collector.MetricsSink)

		// Set pod resource quota capacity as the quota capacity of the namespace.
		for _, quotaResourceType := range metrics.QuotaResources {
			quotaResourceCap := getResourceQuotaCapacity(kubeNamespace, quotaResourceType)
			podMetrics.QuotaCapacity[quotaResourceType] = quotaResourceCap
		}

		// Set the metrics in the map by node and namespace
		nodeName := pod.Spec.NodeName
		if nodeName == "" { //ignore the pod whose node name is not found
			glog.Errorf("Unknown node %s for the pod %s", nodeName, pod.Name)
			continue
		}
		podCollectionMap.addPodMetric(pod.Name, nodeName, kubeNamespace.Name, podMetrics)
	}
	return podCollectionMap
}

// Return the capacity for the resource quota in a namespace
func getResourceQuotaCapacity(namespaceEntity *repository.KubeNamespace, quotaResourceType metrics.ResourceType,
) float64 {
	allocationResource, err := namespaceEntity.GetAllocationResource(quotaResourceType)
	if err != nil { //compute limit is not set
		glog.Errorf("Error getting allocation resource for %s from namespace entity %s: %v", quotaResourceType,
			namespaceEntity.Name, err)
		return repository.DEFAULT_METRIC_CAPACITY_VALUE
	}
	return allocationResource.Capacity
}

// Create PodMetrics for the given pod.
// Amount of quota resources bought from the quota provider is equal to the aggregated compute resource limits and
// requests of all containers of the given pod.
func createPodMetrics(pod *v1.Pod, namespace string, metricsSink *metrics.EntityMetricSink) *repository.PodMetrics {
	podKey := util.PodKeyFunc(pod)

	// quota metrics for the pod
	podMetrics := repository.NewPodMetrics(pod.Name, namespace, pod.Spec.NodeName)
	podMetrics.PodKey = podKey

	totalCPULimits, totalCPURequests, totalMemLimits, totalMemRequests := computeAggregatedResourceLimitsAndRequests(pod)
	// assign compute resource usages to quota resources
	for _, resourceType := range metrics.QuotaResources {
		switch resourceType {
		case metrics.CPULimitQuota:
			podMetrics.QuotaUsed[resourceType] = totalCPULimits
		case metrics.CPURequestQuota:
			podMetrics.QuotaUsed[resourceType] = totalCPURequests
		case metrics.MemoryLimitQuota:
			podMetrics.QuotaUsed[resourceType] = totalMemLimits
		case metrics.MemoryRequestQuota:
			podMetrics.QuotaUsed[resourceType] = totalMemRequests
		}
	}
	for _, resourceType := range metrics.PointsResources {
		metricId := metrics.GenerateEntityResourceMetricUID(metrics.PodType, podKey, resourceType, metrics.Used)
		metric, err := metricsSink.GetMetric(metricId)
		if err != nil {
			if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
				// We simply put in a very low used value on errors retriving a particular metrics
				metric = metrics.NewEntityResourceMetric(metrics.PodType, podKey, resourceType, metrics.Used, 1.0)

			} else {
				glog.Warningf("Cannnot get %s used value from metrics sink for pod %s: %v", resourceType, podKey, err)
				continue
			}
		}
		value := metric.GetValue()
		switch typedValue := value.(type) {
		case []metrics.Point:
			podMetrics.Used[resourceType] = typedValue
		case []metrics.Cumulative:
			var points []metrics.Point
			points, err = util.ConvertCumulativeToPoints(typedValue)
			if err != nil {
				glog.Warningf("Failed to convert cumulative data points to data points for pod %s %v: %v.",
					podKey, resourceType, err)
				continue
			}
			podMetrics.Used[resourceType] = points
		}
	}
	return podMetrics
}

// Collect aggregated compute resources limits and requests of all container of the given pod.
func computeAggregatedResourceLimitsAndRequests(pod *v1.Pod) (float64, float64, float64, float64) {
	totalCPULimits := 0.0
	totalCPURequests := 0.0
	totalMemLimits := 0.0
	totalMemRequests := 0.0
	for _, container := range pod.Spec.Containers {
		// Compute resource limits
		limits := container.Resources.Limits
		totalCPULimits += float64(limits.Cpu().MilliValue())
		totalMemLimits += util.Base2BytesToKilobytes(float64(limits.Memory().Value()))
		// Compute resource requests
		requests := container.Resources.Requests
		totalCPURequests += float64(requests.Cpu().MilliValue())
		totalMemRequests += util.Base2BytesToKilobytes(float64(requests.Memory().Value()))
	}
	return totalCPULimits, totalCPURequests, totalMemLimits, totalMemRequests
}

func (collector *MetricsCollector) CollectPodVolumeMetrics() []*repository.PodVolumeMetrics {
	var podVolumeMetricsCollection []*repository.PodVolumeMetrics

	var podToVolsMap map[string][]repository.MountedVolume
	if collector.Cluster.PodToVolumesMap == nil {
		return podVolumeMetricsCollection
	}
	podToVolsMap = collector.Cluster.PodToVolumesMap

	metricsSink := collector.MetricsSink
	//Iterate over all the pods in the collection
	for _, pod := range collector.PodList {
		podKey := util.PodKeyFunc(pod)
		podVols, exists := podToVolsMap[podKey]
		if !exists {
			continue
		}

		for _, podVol := range podVols {
			podVolumeMetric := repository.PodVolumeMetrics{}
			volKey := util.PodVolumeMetricId(podKey, podVol.MountName)
			found := false

			metricUID := metrics.GenerateEntityResourceMetricUID(metrics.PodType, volKey,
				metrics.StorageAmount, metrics.Used)
			metric, _ := metricsSink.GetMetric(metricUID)
			if metric != nil && metric.GetValue() != nil {
				podVolumeMetric.Used = metric.GetValue().(float64)
				found = true
			}

			metricUID = metrics.GenerateEntityResourceMetricUID(metrics.PodType, volKey,
				metrics.StorageAmount, metrics.Capacity)
			metric, _ = metricsSink.GetMetric(metricUID)
			if metric != nil && metric.GetValue() != nil {
				podVolumeMetric.Capacity = metric.GetValue().(float64)
				found = true
			}

			if found == true {
				podVolumeMetric.QualifiedPodName = podKey
				podVolumeMetric.MountName = podVol.MountName
				podVolumeMetric.Volume = podVol.UsedVolume
				podVolumeMetricsCollection = append(podVolumeMetricsCollection, &podVolumeMetric)
			}
		}
	}
	return podVolumeMetricsCollection
}

// CollectNamespaceMetrics creates namespace metrics for all the namespace entities and set the quota sold usage handled
// by this metric collector.
// Quota resources sold usage of a namespace is equal to the sum of quota resource usages for all the pods running in
// the namespace.
func (collector *MetricsCollector) CollectNamespaceMetrics(podCollection PodMetricsByNodeAndNamespace) []*repository.NamespaceMetrics {
	// collect the cpu frequency metrics from the sink for all the nodes handled by this collector
	collector.collectNodeFrequencies()

	var namespaceMetricsList []*repository.NamespaceMetrics

	// Create namespaceMetrics for each namespace in the cluster
	// This will ensure that the metrics object is created for
	// -- namespaces that have no pods running on some of the nodes
	// -- namespace that have no pods deployed in them
	for namespace := range collector.Cluster.NamespaceMap {
		namespaceMetrics := repository.CreateDefaultNamespaceMetrics(namespace)
		// create quota sold used for each namespace handled by this metric collector
		node := collector.Node

		// list of pods on this namespace on this node
		podMetricsList, exists := podCollection[node.Name][namespace]
		if !exists {
			glog.V(4).Infof("No pod metrics for namespace %s on node %s",
				namespace, node.Name)
			continue
		}

		glog.V(4).Infof("Collecting metrics for "+
			"Namespace: %s on Node: %s with Pods: %s",
			namespace, node.Name, podMetricsList.getPodNames())

		// sum the usages for all the pods in this namespace and node
		podQuotaUsed := podMetricsList.SumQuotaUsage()
		podUsed := podMetricsList.SumUsage()

		// usages for the quota sold from this node
		// is added to the usages from other nodes
		namespaceMetrics.AggregateQuotaUsed(podQuotaUsed)
		namespaceMetrics.AggregateUsed(podUsed)

		namespaceMetricsList = append(namespaceMetricsList, namespaceMetrics)
	}
	return namespaceMetricsList
}

// Get the CPU processor frequency values for the nodes from the Metrics sink
func (collector *MetricsCollector) collectNodeFrequencies() {
	kubeNodes := collector.Cluster.NodeMap
	node := collector.Node
	key := util.NodeKeyFunc(node)
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, key, metrics.CpuFrequency)
	cpuFrequencyMetric, err := collector.MetricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
			// We simply put in the default value for node cpu freq
			cpuFrequencyMetric = metrics.NewEntityStateMetric(metrics.NodeType, util.NodeKeyFunc(node), metrics.CpuFrequency, float64(2600))

		} else {
			glog.Errorf("Failed to get cpu frequency from sink for node %s: %v", key, err)
			return
		}
	}
	if cpuFrequencyMetric == nil {
		glog.Errorf("null cpu frequency from sink for node %s", key)
		return
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	kubeNode := kubeNodes[node.Name]
	kubeNode.NodeCpuFrequency = cpuFrequency
	glog.V(4).Infof("Node %s cpu frequency is %f",
		kubeNode.Name, kubeNode.NodeCpuFrequency)

}

func (collector *MetricsCollector) collectNodeFrequency(node *v1.Node) {
	kubeNodes := collector.Cluster.NodeMap

	key := util.NodeKeyFunc(node)
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, key, metrics.CpuFrequency)
	cpuFrequencyMetric, err := collector.MetricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		glog.Errorf("Failed to get cpu frequency from sink for node %s: %v", key, err)
		return
	}
	if cpuFrequencyMetric == nil {
		glog.Errorf("null cpu frequency from sink for node %s", key)
		return
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	kubeNode := kubeNodes[node.Name]
	kubeNode.NodeCpuFrequency = cpuFrequency
	glog.V(4).Infof("Node %s cpu frequency is %f",
		kubeNode.Name, kubeNode.NodeCpuFrequency)
}
