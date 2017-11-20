package worker

import (
	"k8s.io/client-go/pkg/api/v1"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/golang/glog"
)

// Collects allocation capacity and used metrics from the MetricSink and the associated Quotas
type MetricsCollector struct {
	PodList     []*v1.Pod
	MetricsSink *metrics.EntityMetricSink
	Cluster     *repository.ClusterSummary
}

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
func (podList PodMetricsList) GetAllocationUsage() map[metrics.ResourceType]float64 {
	allocationResources := make(map[metrics.ResourceType]float64)

	podNames := ""
	for _, pod := range podList {
		if pod == nil {
			fmt.Printf("null pod")
			continue
		}
		podNames = podNames + "," + pod.PodName	//TODO: dependency on podName
		allocationMap := pod.AllocationUsed
		for _, resourceType := range metrics.ComputeAllocationResources {
			used, exists := allocationMap[resourceType]
			if !exists {	//cannot find allocation resource usage,
				fmt.Printf("%s: cannot find usage for resource %s\n", pod.PodName, resourceType)
				used = 0.0
			}
			usedSum, found := allocationResources[resourceType]
			if !found {
				usedSum = used
			} else {
				usedSum += used
			}
			allocationResources[resourceType] = usedSum
		}
	}

	glog.V(4).Infof("Collected allocation resources for pod collection %s\n", podNames)
	for rt, used := range allocationResources {
		glog.V(4).Infof("\t rt=%s used=%f\n", rt, used)
	}
	return allocationResources
}

// -------------------------------------------------------------------------------------------------
func (collector *MetricsCollector) CollectPodMetrics() PodMetricsByNodeAndQuota {
	podCollectionMap := make(PodMetricsByNodeAndQuota)
	for _, pod := range collector.PodList {
		// Find quota entity for the pod if available
		quota := collector.Cluster.GetQuota(pod.ObjectMeta.Namespace)
		if quota == nil {
			//ignore the pod not associated with a quota
			continue //eg. default namespace
		}
		podMetrics := collector.createPodMetrics(pod, quota.Name)

		// Get Map by node
		nodeName := pod.Spec.NodeName
		if nodeName == "" {//ignore the pod whose node name is not found
			fmt.Printf("Unknown node for the pod %s:%s\n", pod.Name, nodeName)
			continue //eg. default namespace
		}
		podCollectionMap.addPodMetric(pod.Name, nodeName, quota.Name, podMetrics)
	}
	return podCollectionMap
}

// TODO: unit test this
func (collector *MetricsCollector) createPodMetrics(pod *v1.Pod, quotaName string) *repository.PodMetrics {
	etype := metrics.PodType
	podKey := util.PodKeyFunc(pod)

	podMetrics := &repository.PodMetrics{
		Pod: pod,
		PodName: pod.Name,
		QuotaName: quotaName,
		NodeName: pod.Spec.NodeName,
		PodKey: podKey,
		AllocationUsed: make(map[metrics.ResourceType]float64),
	}

	// pod allocation metrics for the pod
	for _, resourceType  := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[resourceType]
		if !exists {
			continue
		}
		// allocation used is same as compute used
		computeUsedMetricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey,
			computeType, metrics.Used)
		computeUsedMetric, _ := collector.MetricsSink.GetMetric(computeUsedMetricUID)
		if computeUsedMetric != nil && computeUsedMetric.GetValue() != nil {
			podMetrics.AllocationUsed[resourceType] =
				computeUsedMetric.GetValue().(float64)
		} else {
			podMetrics.AllocationUsed[resourceType] = 0.0
			fmt.Printf("[[[[[ %s: cannot find usage for compute resource %s ]]]]]\n", pod.Name, resourceType)
		}
	}
	return podMetrics
}

func (podCollectionMap PodMetricsByNodeAndQuota) addPodMetric(podName, nodeName, quotaName string, podMetrics *repository.PodMetrics) {
	podsByQuotaMap, exists := podCollectionMap[nodeName]
	if !exists {
		podCollectionMap[nodeName] = make(map[string]PodMetricsList)
	}
	podsByQuotaMap, _ = podCollectionMap[nodeName]

	_, exists = podsByQuotaMap[quotaName]
	if !exists {
		podsByQuotaMap[quotaName] = PodMetricsList{}
	}
	podMetricsList, _ := podsByQuotaMap[quotaName]
	podMetricsList = append(podMetricsList, podMetrics)
	podsByQuotaMap[quotaName] = podMetricsList
	podCollectionMap[nodeName] = podsByQuotaMap
	glog.V(4).Infof("Created pod metrics for %s, quota=%s, node=%s\n", podName, quotaName, nodeName)
}

func (collector *MetricsCollector) CollectNodeMetrics(podCollection PodMetricsByNodeAndQuota) NodeMetricsCollection {
	nodeMetricsCollection := make(map[string]*repository.NodeMetrics)
	//sum of usages of pods (with quotas) on nodes
	for  nodeName, podsByQuotaMap  := range podCollection {
		node := collector.Cluster.NodeMap[nodeName]
		if node == nil {
			fmt.Printf("Null node api object for %s\n", nodeName)
			continue
		}

		// sum the pod usages across all quotas for this node
		var collectivePodMetricsList PodMetricsList // across all quotas
		for _, podMetricsList := range podsByQuotaMap {
			collectivePodMetricsList = append(collectivePodMetricsList, podMetricsList...)
		}
		glog.V(4).Infof("collecting pod allocation metrics for Node %s\n", nodeName)
		nodeMetrics := collector.createNodeMetrics(node, collectivePodMetricsList)

		nodeMetricsCollection[nodeName] = nodeMetrics
	}
	return nodeMetricsCollection
}

func (collector *MetricsCollector) createNodeMetrics(node *v1.Node, collectivePodMetricsList PodMetricsList) *repository.NodeMetrics {
	// allocation used by the pods running on the node
	podAllocationUsed := collectivePodMetricsList.GetAllocationUsage()

	// Create new metrics in the sink for the allocation sold by this node
	entityType := metrics.NodeType
	nodeKey := util.NodeKeyFunc(node)

	allocationCap := make(map[metrics.ResourceType]float64)
	for _, allocationResource := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[allocationResource]
		if !exists {
			continue
		}

		computeCapMetricUID := metrics.GenerateEntityResourceMetricUID(
			entityType, nodeKey, computeType, metrics.Capacity)
		computeCapMetric, _ := collector.MetricsSink.GetMetric(computeCapMetricUID)
		if computeCapMetric != nil &&  computeCapMetric.GetValue() != nil {
			allocationCap[allocationResource] = computeCapMetric.GetValue().(float64)
		} else {
			allocationCap[allocationResource] = 0.0
			fmt.Printf("[[[[[ %s: cannot find usage for compute resource %s ]]]]]\n", node.Name, computeType)
		}

	}
	nodeMetric := &repository.NodeMetrics{
		NodeName: node.Name,
		NodeKey: nodeKey,
		AllocationUsed: podAllocationUsed,
		AllocationCap: allocationCap,
	}
	return nodeMetric
}

func (collector *MetricsCollector) CollectQuotaMetrics(podCollection PodMetricsByNodeAndQuota) []*repository.QuotaMetrics {
	var quotaMetricsList []*repository.QuotaMetrics

	for nodeName, podsByQuotaMap  := range podCollection {
		for quotaName, podMetricsList := range podsByQuotaMap {
			glog.V(4).Infof("collecting pod allocation metrics for Node:%s Quota:%s\n",
						nodeName, quotaName)
			// sum the usages for all the pods in this quota
			podAllocationUsed := podMetricsList.GetAllocationUsage()

			quotaMetrics := &repository.QuotaMetrics{
				QuotaName: quotaName,
				AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
			}
			nodeUID, exists := collector.Cluster.NodeNameUIDMap[nodeName]
			if exists {
				quotaMetrics.AllocationBoughtMap[nodeUID] = podAllocationUsed
			}
			quotaMetricsList = append(quotaMetricsList, quotaMetrics)
		}
	}

	return quotaMetricsList
}



// Compute metrics for the pod - TODO: HOLD on modifying the compute capacities for a pod based on the limit ranges in the namespace
// ------------------------------------------
//quotaCpuCapacity := quota.GetComputeResourceCapacity(metrics.CPU)
//if quotaCpuCapacity > 0 {
//	metricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey, metrics.CPU, metrics.Capacity)
//	metric, _ := collector.MetricsSink.GetMetric(metricUID)
//	oldCpuCap := metric.GetValue().(float64)
//	if oldCpuCap > quotaCpuCapacity {
//		fmt.Printf("%s updating cpu capacity from %f to %f\n", pod.Name, oldCpuCap, quotaCpuCapacity)
//		cpuMetric := metrics.NewEntityResourceMetric(etype, podKey, metrics.CPU,
//			metrics.Capacity, quotaCpuCapacity)
//		collector.MetricsSink.UpdateMetricEntry(cpuMetric)
//
//		metricUID = metrics.GenerateEntityResourceMetricUID(etype, podKey, metrics.CPU, metrics.Capacity)
//		metric, _ = collector.MetricsSink.GetMetric(metricUID)
//		fmt.Printf("%s new cpu capacity %f\n", pod.Name, metric.GetValue().(float64))
//	}
//}
//
//quotaMemCapacity := quota.GetComputeResourceCapacity(metrics.Memory)
//if quotaMemCapacity > 0 {
//	metricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey, metrics.Memory, metrics.Capacity)
//	metric, _ := collector.MetricsSink.GetMetric(metricUID)
//	oldMemCap := metric.GetValue().(float64)
//	if oldMemCap > quotaMemCapacity {
//		fmt.Printf("%s updating mem capacity from %f to %f\n", pod.Name, oldMemCap, quotaMemCapacity)
//		memMetric := metrics.NewEntityResourceMetric(etype, podKey,
//			metrics.Memory, metrics.Capacity, quotaMemCapacity)
//		collector.MetricsSink.UpdateMetricEntry(memMetric)
//
//		metricUID = metrics.GenerateEntityResourceMetricUID(etype, podKey, metrics.Memory, metrics.Capacity)
//		metric, _ = collector.MetricsSink.GetMetric(metricUID)
//		fmt.Printf("%s new mem capacity %f\n", pod.Name, metric.GetValue().(float64))
//	}
//}
// ---------------------------------------------------------------------------------
