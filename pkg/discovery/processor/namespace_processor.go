package processor

import (
	"github.com/golang/glog"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

// Class to query the multiple namespace objects data from the Kubernetes API server
// and create the resource quota for each
type NamespaceProcessor struct {
	ClusterInfoScraper cluster.ClusterScraperInterface
	KubeCluster        *repository.KubeCluster
}

func NewNamespaceProcessor(kubeClient cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *NamespaceProcessor {
	return &NamespaceProcessor{
		ClusterInfoScraper: kubeClient,
		KubeCluster:        kubeCluster,
	}
}

// Query the Kubernetes API Server and Get the Namespace objects
func (p *NamespaceProcessor) ProcessNamespaces() map[string]*api.Namespace {
	clusterName := p.KubeCluster.Name
	namespaceList, err := p.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		glog.Errorf("Failed to get namespaces for cluster %s: %v.", clusterName, err)
		return nil
	}
	glog.V(2).Infof("There are %d namespaces.", len(namespaceList))

	quotaMap, err := p.ClusterInfoScraper.GetNamespaceQuotas()
	if err != nil {
		glog.Errorf("Failed to list all quotas in the cluster %s: %v.", clusterName, err)
		return nil
	}
	glog.V(2).Infof("There are %d resource quotas.", len(quotaMap))

	namespaceMap := make(map[string]*repository.KubeNamespace)
	kubeNamespaceMap := make(map[string]*api.Namespace)
	for _, item := range namespaceList {
		// Create default namespace object
		kubeNamespaceUID := string(item.UID)
		kubeNamespace := repository.CreateDefaultKubeNamespace(clusterName, item.Name, kubeNamespaceUID)
		kubeNamespace.Labels = item.GetLabels()
		kubeNamespace.Annotations = item.GetAnnotations()

		// update the default quota limits using the defined resource quota objects
		quotaList, hasQuota := quotaMap[item.Name]
		if hasQuota {
			kubeNamespace.QuotaList = quotaList
			kubeNamespace.ReconcileQuotas(quotaList)
		}

		namespaceMap[item.Name] = kubeNamespace
		kubeNamespaceMap[item.Name] = item
		glog.V(4).Infof("Created namespace entity: %s.", kubeNamespace.String())

	}
	p.KubeCluster.NamespaceMap = namespaceMap

	return kubeNamespaceMap
}
