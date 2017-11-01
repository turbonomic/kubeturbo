package worker

import (
	"k8s.io/client-go/pkg/api/v1"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/golang/glog"
)

// Collects usage values for a group of pods
type MetricsCollector struct {
	PodList     []*v1.Pod
	MetricsSink *metrics.EntityMetricSink
	Cluster     *repository.KubeCluster
	NodeNameMap map[string]*v1.Node
}

type PodMetricsList []*repository.PodMetrics
type PodMetricsByNodeAndQuota map[string]map[string]PodMetricsList

// Returns the sum of compute resources for all the pods in the collection
func (podList PodMetricsList) GetComputeUsage() map[metrics.ResourceType]float64 {
	computeResources := make(map[metrics.ResourceType]float64)

	podNames := ""
	for _, pod := range podList {
		podNames = podNames + "," + pod.PodName
		computeMap := pod.ComputeUsed
		for _, resourceType := range metrics.ComputeResources {
			used, exists := computeMap[resourceType]
			if !exists {
				fmt.Printf("%s: cannot find usage for resource %s\n", pod.Pod.Name, resourceType)
				continue
			}
			usedSum, found := computeResources[resourceType]
			if !found {
				usedSum = used
			} else {
				usedSum += used
			}
			computeResources[resourceType] = usedSum
		}
	}
	fmt.Printf("Collected compute resources for pod collection %s\n", podNames)
	for rt, used := range computeResources {
		fmt.Printf("\t rt=%s used=%f\n", rt, used)
	}
	return computeResources
}

// Returns the sum of allocation resources for all the pods in the collection
func (podList PodMetricsList) GetAllocationUsage() map[metrics.ResourceType]float64 {
	allocationResources := make(map[metrics.ResourceType]float64)

	podNames := ""
	for _, pod := range podList {
		podNames = podNames + "," + pod.PodName
		computeMap := pod.AllocationUsed
		for _, resourceType := range metrics.ComputeAllocationResources {
			used, exists := computeMap[resourceType]
			if !exists {
				fmt.Printf("%s: cannot find usage for resource %s\n", pod.Pod.Name, resourceType)
				continue
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

func (podCollectionMap PodMetricsByNodeAndQuota) addPodMetric(nodeName, quotaName string, podMetrics *repository.PodMetrics) {
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
}

// Collects compute and allocation capacity and used metrics from the MetricSink and the associated Quotas
func (collector *MetricsCollector) CollectPodMetrics() PodMetricsByNodeAndQuota {
	podCollectionMap := make(PodMetricsByNodeAndQuota)
	for _, pod := range collector.PodList {
		podId := string(pod.UID)
		// Find quota entity for the pod if available
		quota := collector.Cluster.GetQuota(pod.ObjectMeta.Namespace)
		if quota == nil {
			//ignore the pod not associated with a quota
			continue //eg. default namespace
		}
		etype := metrics.PodType
		podKey := util.PodKeyFunc(pod)

		quotaName := quota.Name
		podMetrics := &repository.PodMetrics{
			Pod: pod,
			PodId: podId,
			PodName: pod.Name,
			QuotaName: quota.Name,
			NodeName: pod.Spec.NodeName,
			PodKey: podKey,
			ComputeUsed: make(map[metrics.ResourceType]float64),
			ComputeCap: make(map[metrics.ResourceType]float64),
			AllocationUsed: make(map[metrics.ResourceType]float64),
		}

		// Compute metrics for the pod - TODO: HOLD
		// ------------------------------------------
		//quotaCpuCapacity := quota.GetCPUCapacity()
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
		//quotaMemCapacity := quota.GetMemoryCapacity()
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

		// pod compute resources - used and capacity
		for _, resourceType := range metrics.ComputeResources {
			computeCapMetricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey,
								resourceType, metrics.Capacity)
			computeCapMetric, _ := collector.MetricsSink.GetMetric(computeCapMetricUID)
			if computeCapMetric != nil && computeCapMetric.GetValue() != nil {
				podMetrics.ComputeCap[resourceType] =
					computeCapMetric.GetValue().(float64)
			}

			computeUsedMetricUID := metrics.GenerateEntityResourceMetricUID(etype, podKey,
									resourceType, metrics.Used)
			computeUsedMetric, _ := collector.MetricsSink.GetMetric(computeUsedMetricUID)
			if computeUsedMetric != nil && computeUsedMetric.GetValue() != nil {
				podMetrics.ComputeUsed[resourceType] =
					computeUsedMetric.GetValue().(float64)
			}
		}

		// Create Allocation metrics for the pod
		for _, resourceType  := range metrics.ComputeAllocationResources {
			computeType, exists := metrics.AllocationToComputeMap[resourceType]
			if !exists {
				continue
			}
			// allocation used is same as compute used
			podMetrics.AllocationUsed[resourceType] = podMetrics.ComputeUsed[computeType]
		}

		// Get Map by node
		nodeName := pod.Spec.NodeName
		if nodeName == "" {//ignore the pod whose node name is not found
			fmt.Printf("Unknown node for the pod %s:%s\n", pod.Name, nodeName)
			continue //eg. default namespace
		}
		_, exist := podCollectionMap[nodeName]
		if !exist {
			podCollectionMap[nodeName] = make(map[string]PodMetricsList)
		}
		podsByQuotaMap, _ := podCollectionMap[nodeName]
		_, exists := podsByQuotaMap[quotaName]
		if !exists {
			podsByQuotaMap[quotaName] = PodMetricsList{}
		}
		podMetricsList, _ := podsByQuotaMap[quotaName]
		podMetricsList = append(podMetricsList, podMetrics)
		podsByQuotaMap[quotaName] = podMetricsList
		podCollectionMap[nodeName] = podsByQuotaMap
		glog.V(4).Infof("Created pod metrics for %s, quota=%s, node=%s\n", pod.Name, quotaName, nodeName)
	}
	return podCollectionMap
}

type NodeMetricsCollection map[string]*repository.NodeMetrics
func (collector *MetricsCollector) CollectNodeMetrics(podCollection PodMetricsByNodeAndQuota) NodeMetricsCollection {
	nodeMetricsCollection := make(map[string]*repository.NodeMetrics)
	//sum of usages of pods (with quotas) on nodes
	entityType := metrics.NodeType
	for  nodeName, podsByQuotaMap  := range podCollection {

		// sum the pod usages across all quotas for this node
		podAllocationUsed := make(map[metrics.ResourceType]float64)
		var collectivePodMetricsList PodMetricsList // across all quotas
		for _, podMetricsList := range podsByQuotaMap {
			collectivePodMetricsList = append(collectivePodMetricsList, podMetricsList...)
		}
		fmt.Printf("collection pod allocation metrics for Node %s\n", nodeName)
		podAllocationUsed = collectivePodMetricsList.GetAllocationUsage()

		// Get Node Compute
		node := collector.NodeNameMap[nodeName]
		nodeKey := util.NodeKeyFunc(node)
		computeCap := make(map[metrics.ResourceType]float64)
		for _, resourceType := range metrics.ComputeResources {
			computeCapMetricUID := metrics.GenerateEntityResourceMetricUID(
				entityType, nodeKey, resourceType, metrics.Capacity)
			computeCapMetric, _ := collector.MetricsSink.GetMetric(computeCapMetricUID)
			if computeCapMetric == nil {
				fmt.Printf("%s: Cannot find used metrics for %s\n", node.Name, computeCapMetricUID)
				continue
			}
			computeCap[resourceType] = computeCapMetric.GetValue().(float64)
		}

		// Create new metrics in the sink for the allocation sold by this node
		allocationCap := make(map[metrics.ResourceType]float64)
		for _, allocationResource := range metrics.ComputeAllocationResources {
			computeType, exists := metrics.AllocationToComputeMap[allocationResource]
			if !exists {
				continue
			}
			allocationCap[allocationResource] = computeCap[computeType]
		}
		nodeMetric := &repository.NodeMetrics{
			NodeName: nodeName,
			NodeKey: nodeKey,
			ComputeCap: computeCap,
			AllocationUsed: podAllocationUsed,
			AllocationCap: allocationCap,
		}
		nodeMetricsCollection[nodeName] = nodeMetric
	}
	return nodeMetricsCollection
}

func (collector *MetricsCollector) CollectQuotaMetrics(podCollection PodMetricsByNodeAndQuota) []*repository.QuotaMetrics {
	var quotaMetricsList []*repository.QuotaMetrics

	for nodeName, podsByQuotaMap  := range podCollection {
		for quotaName, podMetricsList := range podsByQuotaMap {
			fmt.Printf("collection pod allocation metrics for Node:%s Quota:%s\n",
						nodeName, quotaName)
			// sum the usages for all the pods in this quota
			podAllocationUsed := podMetricsList.GetAllocationUsage()

			quotaMetrics := &repository.QuotaMetrics{
				QuotaName: quotaName,
				AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
			}
			quotaMetrics.AllocationBoughtMap[nodeName] = podAllocationUsed
			quotaMetricsList = append(quotaMetricsList, quotaMetrics)
		}
	}

	return quotaMetricsList
}
