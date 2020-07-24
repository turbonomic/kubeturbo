package worker

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	// Combine quota discovery results from different nodes
	namespaceMetricsMap := make(map[string]*repository.NamespaceMetrics)

	// combine namespace metrics results from different discovery workers
	// each worker will provide the allocation bought for a set of nodes and
	// the allocation used for the pods running on those nodes
	for _, namespaceMetrics := range namespaceMetricsList {
		existingMetric, exists := namespaceMetricsMap[namespaceMetrics.Namespace]
		if !exists {
			// first time that this quota is seen
			namespaceMetricsMap[namespaceMetrics.Namespace] = namespaceMetrics
			continue
		}
		// merge the pod usage from this namespace metrics into the existing namespace metrics
		existingMetric.UpdateQuotaSoldUsed(namespaceMetrics.QuotaSoldUsed)
	}

	kubeNodes := worker.Cluster.Nodes
	var nodeUIDs []string
	var totalNodeFrequency float64
	activeNodeCount := 0
	for _, node := range kubeNodes {
		nodeActive := util.NodeIsReady(node.Node) && util.NodeIsSchedulable(node.Node)
		if nodeActive {
			nodeUIDs = append(nodeUIDs, node.UID)
			totalNodeFrequency += node.NodeCpuFrequency
			activeNodeCount++
		}

	}
	averageNodeFrequency := totalNodeFrequency / float64(activeNodeCount)
	glog.V(2).Infof("Average cluster node cpu frequency in MHz: %f", averageNodeFrequency)

	// Create the quota resources for all kubeNamespace entities using the metrics object
	for namespace, kubeNamespaceEntity := range worker.Cluster.NamespaceMap {
		// the namespace metrics
		namespaceMetrics, exists := namespaceMetricsMap[namespace]
		if !exists {
			glog.Errorf("Missing quota metrics for namespace %s", namespace)
			continue
		}
		kubeNamespaceEntity.AverageNodeCpuFrequency = averageNodeFrequency

		// Create sold allocation commodity for the types that are not defined in the kubeNamespace objects
		for resourceType, used := range namespaceMetrics.QuotaSoldUsed {
			existingResource, _ := kubeNamespaceEntity.GetResource(resourceType)
			// Set used value collected from namespaceMetrics to kubeNamespaceEntity, which is the sum of limits/request
			// from all running containers on this namespace.
			// If resource is CPU type, used value has already been converted from cores to MHz when collecting namespace
			// metrics in metrics_collector.
			existingResource.Used = used
		}
	}

	for _, kubeNamespaceEntity := range worker.Cluster.NamespaceMap {
		glog.V(4).Infof("Discovered namespace entity: %s", kubeNamespaceEntity)
	}

	// Create DTOs for each namespace entity
	namespaceEntityDTOBuilder := dtofactory.NewNamespaceEntityDTOBuilder(worker.Cluster.NamespaceMap)
	namespaceEntityDtos, _ := namespaceEntityDTOBuilder.BuildEntityDTOs()
	return namespaceEntityDtos, nil
}
