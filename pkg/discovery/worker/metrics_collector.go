package worker

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"k8s.io/client-go/pkg/api/v1"
)

// Collects allocation metrics for quotas, nodes and pods using the compute resource usages for pods
type MetricsCollector struct {
	NodeList    []*v1.Node
	PodList     []*v1.Pod
	MetricsSink *metrics.EntityMetricSink
	Cluster     *repository.ClusterSummary
}

// Abstraction for a list of PodMetrics
type PodMetricsList []*repository.PodMetrics
type PodMetricsByNodeAndQuota map[string]map[string]PodMetricsList

type NodeMetricsCollection map[string]*repository.NodeMetrics

func (podList PodMetricsList) GetPods() string {
	podNames := ""
	for _, pod := range podList {
		if pod != nil {
			podNames = podNames + "," + pod.PodName
		}
	}
	return podNames
}

// Returns the sum of allocation resources for all the pods in the collection
func (podMetricsList PodMetricsList) SumAllocationUsage() map[metrics.ResourceType]float64 {
	allocationResourcesSum := make(map[metrics.ResourceType]float64)

	// first sum the compute resources used
	computeResourcesSum := make(map[metrics.ResourceType]float64)
	for _, computeType := range metrics.ComputeResources {
		var totalUsed float64
		for _, podMetrics := range podMetricsList {
			if podMetrics.ComputeUsed == nil {
				continue
			}
			// check if allocation bought and the resource type exists
			used, exists := podMetrics.ComputeUsed[computeType]
			if !exists {
				continue
			}
			totalUsed += used
		}
		computeResourcesSum[computeType] = totalUsed
	}

	// pod allocation resource usage is same as its corresponding compute resource usage
	for _, allocationType := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[allocationType]
		if !exists {
			glog.Errorf("cannot find corresponding compute type for %s", allocationType)
			continue
		}
		allocationResourcesSum[allocationType] = computeResourcesSum[computeType]
	}

	glog.V(4).Infof("Collected allocation resources for pod collection %s\n", podMetricsList.GetPods())
	for rt, used := range allocationResourcesSum {
		glog.V(4).Infof("\t rt=%s used=%f\n", rt, used)
	}
	return allocationResourcesSum
}

func (podCollectionMap PodMetricsByNodeAndQuota) addPodMetric(podName, nodeName, quotaName string,
	podMetrics *repository.PodMetrics) {
	podsByQuotaMap, exists := podCollectionMap[nodeName]
	if !exists {
		// create the map for this node
		podCollectionMap[nodeName] = make(map[string]PodMetricsList)
	}
	podsByQuotaMap, _ = podCollectionMap[nodeName]

	_, exists = podsByQuotaMap[quotaName]
	if !exists {
		// create the map for this quota
		podsByQuotaMap[quotaName] = PodMetricsList{}
	}
	podMetricsList, _ := podsByQuotaMap[quotaName]
	podMetricsList = append(podMetricsList, podMetrics)
	podsByQuotaMap[quotaName] = podMetricsList
	podCollectionMap[nodeName] = podsByQuotaMap
	glog.V(4).Infof("Created pod metrics for %s, quota=%s, node=%s\n", podName, quotaName, nodeName)
}

// -------------------------------------------------------------------------------------------------

// Create Pod metrics by selecting the pod compute resource usages from the metrics sink.
// The PodMetrics are organized in a map by node and quota
func (collector *MetricsCollector) CollectPodMetrics() PodMetricsByNodeAndQuota {
	podCollectionMap := make(PodMetricsByNodeAndQuota)
	// Iterate over all pods
	for _, pod := range collector.PodList {
		// Find quota entity for the pod if available
		quota := collector.Cluster.GetQuota(pod.ObjectMeta.Namespace)
		if quota == nil {
			//ignore the pod not associated with a quota
			glog.Errorf("Nil quota for the pod %s:%s\n", pod.Name, pod.ObjectMeta.Namespace)
			continue
		}
		// pod allocation metrics for the pod
		podMetrics := createPodMetrics(pod, quota.Name, collector.MetricsSink)

		// collect pod compute capacities when the namespace
		// has defined quota limits for the compute resources
		etype := metrics.PodType
		for _, computeType := range metrics.ComputeResources {
			computeCapMetricUID := metrics.GenerateEntityResourceMetricUID(etype,
				podMetrics.PodKey,
				computeType, metrics.Capacity)
			computeCapMetric, _ := collector.MetricsSink.GetMetric(computeCapMetricUID)
			if computeCapMetric != nil && computeCapMetric.GetValue() != nil {
				podCpuCap := computeCapMetric.GetValue().(float64)

				var quotaComputeCap float64
				quotaComputeCap, err := getQuotaComputeCapacity(quota, computeType)
				if err == nil && quotaComputeCap < podCpuCap {
					podMetrics.ComputeCapacity[computeType] = quotaComputeCap
				}
			}
		}

		// Set the metrics in the map by node and quota
		nodeName := pod.Spec.NodeName
		if nodeName == "" { //ignore the pod whose node name is not found
			glog.Errorf("Unknown node for the pod %s:%s\n", pod.Name, nodeName)
			continue
		}
		podCollectionMap.addPodMetric(pod.Name, nodeName, quota.Name, podMetrics)
	}
	return podCollectionMap
}

func getQuotaComputeCapacity(quotaEntity *repository.KubeQuota, computeType metrics.ResourceType,
) (float64, error) {
	var allocationType metrics.ResourceType
	if computeType == metrics.CPU {
		allocationType = metrics.CPULimit
	} else if computeType == metrics.Memory {
		allocationType = metrics.MemoryLimit
	}
	quotaCompute, err := quotaEntity.GetAllocationResource(allocationType)
	if err != nil {
		return 0.0, err
	}
	return quotaCompute.Capacity, nil
}

// Create PodMetrics for the given pod.
// Amount of Allocation resources bought from the quota is equal to the compute resource usages
// of the pod that is obtained from the metrics sink
func createPodMetrics(pod *v1.Pod, quotaName string, metricsSink *metrics.EntityMetricSink,
) *repository.PodMetrics {
	etype := metrics.PodType
	podKey := util.PodKeyFunc(pod)

	// pod allocation metrics for the pod
	podMetrics := repository.NewPodMetrics(pod.Name, quotaName, pod.Spec.NodeName)
	podMetrics.PodKey = podKey

	// get the compute resource usages
	for _, computeType := range metrics.ComputeResources {
		computeUsedMetricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey,
			computeType, metrics.Used)
		computeUsedMetric, _ := metricsSink.GetMetric(computeUsedMetricUID)
		if computeUsedMetric != nil && computeUsedMetric.GetValue() != nil {
			podMetrics.ComputeUsed[computeType] =
				computeUsedMetric.GetValue().(float64)
		} else {
			glog.Errorf("%s: cannot find usage for compute resource %s\n",
				pod.Name, computeType)
		}
	}
	// assign compute resource usages to allocation resources
	for _, resourceType := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[resourceType]
		if !exists {
			glog.Errorf("cannot find corresponding compute type for %s", resourceType)
			continue
		}
		allocationBought, exists := podMetrics.ComputeUsed[computeType]
		if exists {
			podMetrics.AllocationBought[resourceType] = allocationBought
		} else {
			podMetrics.AllocationBought[resourceType] = 0.0
		}
	}
	return podMetrics
}

// Create Node metrics by adding the pod compute resource usages from the metrics sink.
func (collector *MetricsCollector) CollectNodeMetrics(podCollection PodMetricsByNodeAndQuota,
) NodeMetricsCollection {
	nodeMetricsCollection := make(map[string]*repository.NodeMetrics)

	//Iterate over all the nodes in the collection
	for nodeName, podsByQuotaMap := range podCollection {
		node := collector.Cluster.NodeMap[nodeName]
		if node == nil {
			continue
		}

		// collect the metrics for all the pods running on this node across all quotas
		var collectivePodMetricsList PodMetricsList
		for _, podMetricsList := range podsByQuotaMap {
			collectivePodMetricsList = append(collectivePodMetricsList, podMetricsList...)
		}
		glog.V(4).Infof("collecting metrics for Node %s Pods:%s\n",
			nodeName, collectivePodMetricsList.GetPods())

		nodeMetrics := createNodeMetrics(node, collectivePodMetricsList, collector.MetricsSink)
		nodeMetricsCollection[nodeName] = nodeMetrics
	}
	return nodeMetricsCollection
}

// Create NodeMetrics for the given node.
// Allocation capacity is same as the compute resource capacity. Allocation usage is the sum of allocation
// usages from all the pods running on the node
func createNodeMetrics(node *v1.Node, collectivePodMetricsList PodMetricsList, metricsSink *metrics.EntityMetricSink) *repository.NodeMetrics {
	// allocation usages for the node - sum of allocation usages from all the pods on the node
	podAllocationUsed := collectivePodMetricsList.SumAllocationUsage()

	// allocation capacities for the node is same as the compute resource capacity
	entityType := metrics.NodeType
	nodeKey := util.NodeKeyFunc(node)

	allocationCap := make(map[metrics.ResourceType]float64)
	for _, allocationResource := range metrics.ComputeAllocationResources {
		// get the corresponding compute type resource
		computeType, exists := metrics.AllocationToComputeMap[allocationResource]
		if !exists {
			continue
		}

		computeCapMetricUID := metrics.GenerateEntityResourceMetricUID(
			entityType, nodeKey, computeType, metrics.Capacity)
		computeCapMetric, _ := metricsSink.GetMetric(computeCapMetricUID)
		if computeCapMetric != nil && computeCapMetric.GetValue() != nil {
			allocationCap[allocationResource] = computeCapMetric.GetValue().(float64)
		} else {
			allocationCap[allocationResource] = 0.0
			glog.Warningf("[%s: cannot find usage for compute resource %s ]\n", node.Name, computeType)
		}
	}

	nodeMetric := &repository.NodeMetrics{
		NodeName:       node.Name,
		NodeKey:        nodeKey,
		AllocationUsed: podAllocationUsed,
		AllocationCap:  allocationCap,
	}
	return nodeMetric
}

// Create Quota metrics for all the quotas and set the allocation bought for each node provider
// handled by this metric collector.
// Amount of Allocation resources bought by a quota from each node provider is equal to the
// sum of allocation resource usages for the pods running on that node.
// Allocation resources sold usage of a quota is equal to the sum of allocation resource usages
// for all the pods running in the quota
func (collector *MetricsCollector) CollectQuotaMetrics(podCollection PodMetricsByNodeAndQuota) []*repository.QuotaMetrics {
	// collect the cpu frequency metric for the nodes
	collector.collectNodeFrequencies()

	var nodeUIDs []string
	for _, node := range collector.NodeList {
		nodeUIDs = append(nodeUIDs, string(node.UID))
	}
	var quotaMetricsList []*repository.QuotaMetrics

	// Create quota metrics for each quota in the cluster
	// This will ensure that the metrics object is created for
	// -- namespaces that have no pods running on some of the nodes
	// -- namespace that have not pods deployed in them
	for quotaName, _ := range collector.Cluster.QuotaMap {
		quotaMetrics := repository.CreateDefaultQuotaMetrics(quotaName, nodeUIDs)
		// create allocation bought for each node provider handled by this metric collector
		for _, node := range collector.NodeList {
			kubeNode := collector.Cluster.Nodes[node.Name]
			quotaMetrics.NodeProviders = append(quotaMetrics.NodeProviders, node.Name)
			// list of pods on this node
			podMetricsList, exists := podCollection[node.Name][quotaName]
			if !exists {
				glog.V(4).Infof("%s : no pod metrics for node %s\n",
					quotaName, node.Name)
				continue
			}

			podNames := podMetricsList.GetPods()
			glog.V(4).Infof("collecting metrics for Quota:%s Node:%s Pods:%s\n",
				quotaName, node.Name, podNames)

			// sum the usages for all the pods in this quota
			podAllocationUsed := podMetricsList.SumAllocationUsage()

			// conversion for cpu resource usages from cores to MHz for this list of pods
			for rt, val := range podAllocationUsed {
				if metrics.IsCPUType(rt) && kubeNode.NodeCpuFrequency > 0.0 {
					newVal := val * kubeNode.NodeCpuFrequency
					podAllocationUsed[rt] = newVal
				}
			}

			// allocation bought usage
			quotaMetrics.UpdateAllocationBought(kubeNode.UID, podAllocationUsed)

			// allocation sold usage
			quotaMetrics.UpdateAllocationSold(podAllocationUsed)
		}
		quotaMetricsList = append(quotaMetricsList, quotaMetrics)
	}
	return quotaMetricsList
}

// Get the CPU processor frequency values for the nodes from the Metrics sink
func (collector *MetricsCollector) collectNodeFrequencies() {
	kubeNodes := collector.Cluster.Nodes
	for _, node := range collector.NodeList {
		key := util.NodeKeyFunc(node)
		cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, key, metrics.CpuFrequency)
		cpuFrequencyMetric, err := collector.MetricsSink.GetMetric(cpuFrequencyUID)
		if err != nil {
			glog.Errorf("Failed to get cpu frequency from sink for node %s: %s\n", key, err)
		}
		if cpuFrequencyMetric != nil {
			cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
			kubeNode := kubeNodes[node.Name]
			kubeNode.NodeCpuFrequency = cpuFrequency
			glog.V(4).Infof("%s : kubenode cpu frequency is %f",
				kubeNode.Name, kubeNode.NodeCpuFrequency)
		} else {
			glog.Errorf("null cpu frequency from sink for node %s\n", key)
		}
	}
}
