package processor

import (
	"github.com/golang/glog"
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
func (p *NamespaceProcessor) ProcessNamespaces() {
	clusterName := p.KubeCluster.Name
	namespaceList, err := p.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		glog.Errorf("Failed to get namespaces for cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d namespaces.", len(namespaceList))

	quotaMap, err := p.ClusterInfoScraper.GetNamespaceQuotas()
	if err != nil {
		glog.Errorf("Failed to list all quotas in the cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d resource quotas.", len(quotaMap))

	namespaces := make(map[string]*repository.KubeNamespace)
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

		namespaces[item.Name] = kubeNamespace
		glog.V(4).Infof("Created namespace entity: %s.", kubeNamespace.String())

	}
	p.KubeCluster.NamespaceMap = namespaces
}
