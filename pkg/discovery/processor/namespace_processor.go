package processor

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const(
	vdcPrefix = "k8s-vdc"
)

// Class to query the multiple namespace objects data from the Kubernetes API server
// and create the resource quota for each
type NamespaceProcessor struct {
	ClusterInfoScraper cluster.ClusterScraperInterface
	clusterName        string
	ClusterResources   map[metrics.ResourceType]*repository.KubeDiscoveredResource
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *NamespaceProcessor) ProcessNamespaces() (map[string]*repository.KubeNamespace, error) {
	namespaceList, err := processor.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		return nil, fmt.Errorf("Error getting namespaces for cluster %s:%s\n", processor.clusterName, err)
	}
	glog.V(2).Infof("There are %d namespaces\n", len(namespaceList))

	quotaMap, err := processor.ClusterInfoScraper.GetNamespaceQuotas()
	if err != nil {
		glog.Errorf("failed to list all quotas in the cluster %s: %s", processor.clusterName, err)
	}
	glog.V(2).Infof("There are %d resource quotas\n", len(quotaMap))

	namespaces := make(map[string]*repository.KubeNamespace)
	for _, item := range namespaceList {
		namespace := &repository.KubeNamespace{
			ClusterName: processor.clusterName,
			Name:        item.Name,
		}

		// the default quota object
		quotaUID := fmt.Sprintf("%s-%s", vdcPrefix, item.UID)
		quotaEntity := repository.CreateDefaultQuota(processor.clusterName,
			namespace.Name,
			quotaUID,
			processor.ClusterResources)

		// update the default quota limits using the defined resource quota objects
		quotaList, hasQuota := quotaMap[item.Name]
		if hasQuota {
			quotaEntity.QuotaList = quotaList
			quotaEntity.ReconcileQuotas(quotaList)
		}
		namespace.Quota = quotaEntity
		namespaces[item.Name] = namespace
	}
	return namespaces, nil
}
