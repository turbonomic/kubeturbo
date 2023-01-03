package processor

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

type GitOpsConfigProcessor struct {
	ClusterScraper cluster.ClusterScraperInterface
	KubeCluster    *repository.KubeCluster
}

func NewGitOpsConfigProcessor(clusterScraper cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *GitOpsConfigProcessor {
	return &GitOpsConfigProcessor{
		ClusterScraper: clusterScraper,
		KubeCluster:    kubeCluster,
	}
}

func (p *GitOpsConfigProcessor) ProcessGitOpsConfigs() {
	gitOpsConfigs, err := p.ClusterScraper.GetAllGitOpsConfigurations()
	if err != nil {
		glog.Warningf("Failed to list GitOps configurations: %v", err)
		return
	}
	glog.V(2).Infof("Discovered %v GitOps Configuration Overrides", len(gitOpsConfigs))
	p.KubeCluster.GitOpsConfigurations = gitOpsConfigs
}
