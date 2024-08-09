package worker

import (
	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sQuotasWorkerID string = "ResourceQuotasDiscoveryWorker"
)

// Converts the cluster namespaceEntity and NamespaceMetrics objects to create Namespace DTOs
type k8sNamespaceDiscoveryWorker struct {
	id         string
	Cluster    *repository.ClusterSummary
	stitchType stitching.StitchingPropertyType
}

func Newk8sNamespaceDiscoveryWorker(cluster *repository.ClusterSummary, pType stitching.StitchingPropertyType,
) *k8sNamespaceDiscoveryWorker {
	return &k8sNamespaceDiscoveryWorker{
		Cluster:    cluster,
		id:         k8sQuotasWorkerID,
		stitchType: pType,
	}
}

func (worker *k8sNamespaceDiscoveryWorker) Do(namespaceMetricsList []*repository.NamespaceMetrics,
) ([]*proto.EntityDTO, error) {
	// Combine discovery results from different nodes
	namespaceMetricsMap := make(map[string]*repository.NamespaceMetrics)

	// combine namespace metrics results from different discovery workers
	// each worker will provide the allocation bought for a set of nodes and
	// the used for the pods running on those nodes
	for _, namespaceMetrics := range namespaceMetricsList {
		existingMetric, exists := namespaceMetricsMap[namespaceMetrics.Namespace]
		if !exists {
			// first time that this Namespace is seen
			namespaceMetricsMap[namespaceMetrics.Namespace] = namespaceMetrics
			continue
		}
		// merge the pod usage from this namespace metrics into the existing namespace metrics
		existingMetric.AggregateQuotaUsed(namespaceMetrics.QuotaUsed)
		existingMetric.AggregateUsed(namespaceMetrics.Used)
	}

	kubeNodes := worker.Cluster.NodeMap
	var nodeUIDs []string
	var totalNodeFrequency float64
	activeNodeCount := 0
	for _, node := range kubeNodes {
		nodeActive := util.NodeIsReady(node.Node)
		if nodeActive {
			nodeUIDs = append(nodeUIDs, node.UID)
			totalNodeFrequency += node.NodeCpuFrequency
			activeNodeCount++
		}

	}
	averageNodeFrequency := totalNodeFrequency / float64(activeNodeCount)
	glog.V(2).Infof("Average cluster node cpu frequency in MHz: %f", averageNodeFrequency)
	worker.Cluster.AverageNodeCpuFrequency = averageNodeFrequency

	// Create the quota resources for all kubeNamespace entities using the metrics object
	for namespace, kubeNamespaceEntity := range worker.Cluster.NamespaceMap {
		// the namespace metrics
		namespaceMetrics, exists := namespaceMetricsMap[namespace]
		if !exists {
			glog.Warningf("No quota metrics found for namespace %s", namespace)
			continue
		}
		kubeNamespaceEntity.AverageNodeCpuFrequency = averageNodeFrequency

		// Create bought commodity for the types that are not defined in the kubeNamespace objects
		for resourceType, points := range namespaceMetrics.Used {
			existingResource, err := kubeNamespaceEntity.GetResource(resourceType)
			if err != nil {
				glog.Errorf("No resource found for type %s in namespace %s", resourceType, kubeNamespaceEntity.Namespace)
				continue
			}
			existingResource.Points = points
		}

		// Create sold allocation commodity for the types that are not defined in the kubeNamespace objects
		// Additionally update the used values from requestQuota metrics into the compute request commodities
		for resourceType, used := range namespaceMetrics.QuotaUsed {
			existingResource, _ := kubeNamespaceEntity.GetResource(resourceType)
			// Set used value collected from namespaceMetrics to kubeNamespaceEntity, which is the sum of limits/request
			// from all running containers on this namespace.
			// If resource is CPU type, used value has already been converted from cores to MHz when collecting namespace
			// metrics in metrics_collector.
			existingResource.Used = used

			// We know that the quota values are already aggregated for the namespace. We can directly use the
			// request quota used values as the request used values.
			// Also note that this is updated after block updating bought commodities above, effectively overwriting
			// any value that might have been filled there. Those values are ideally zero because we do not separately
			// aggregate the request values.
			if resourceType == metrics.CPURequestQuota {
				res, _ := kubeNamespaceEntity.GetResource(metrics.CPURequest)
				res.Used = used
			}
			if resourceType == metrics.MemoryRequestQuota {
				res, _ := kubeNamespaceEntity.GetResource(metrics.MemoryRequest)
				res.Used = used
			}
		}
	}

	for _, kubeNamespaceEntity := range worker.Cluster.NamespaceMap {
		glog.V(4).Infof("Discovered namespace entity: %s", kubeNamespaceEntity)
	}

	// Create DTOs for each namespace entity
	namespaceEntityDTOBuilder := dtofactory.NewNamespaceEntityDTOBuilder(worker.Cluster)
	namespaceEntityDtos, _ := namespaceEntityDTOBuilder.BuildEntityDTOs()
	return namespaceEntityDtos, nil
}
