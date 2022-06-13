package processor

import (
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

type TurboPolicyProcessor struct {
	ClusterScraper cluster.ClusterScraperInterface
	KubeCluster    *repository.KubeCluster
}

var (
	spewConfigState = spew.ConfigState{
		DisablePointerAddresses: true,
		DisableCapacities:       true,
		Indent:                  "  ",
		SortKeys:                true,
	}
)

func NewTurbolicyProcessor(clusterScraper cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *TurboPolicyProcessor {
	return &TurboPolicyProcessor{
		ClusterScraper: clusterScraper,
		KubeCluster:    kubeCluster,
	}
}

func (p *TurboPolicyProcessor) ProcessTurboPolicies() {
	sloScalings, err := p.ClusterScraper.GetAllTurboSLOScalings()
	if err != nil {
		glog.Warningf("Failed to list SLOHorizontalScales: %v.", err)
		return
	}
	if len(sloScalings) == 0 {
		glog.V(2).Info("There is no SLOHorizontalScale resource found in the cluster.")
		return
	}
	glog.V(3).Infof("Turbo SLOHorizontalScales: %v.", spewConfigState.Sdump(sloScalings))

	policyBindings, err := p.ClusterScraper.GetAllTurboPolicyBindings()
	if err != nil {
		glog.Warningf("Failed to list PolicyBindings: %v.", err)
		return
	}
	if len(policyBindings) == 0 {
		glog.V(2).Info("There is no PolicyBinding resource found in the cluster.")
		return
	}
	glog.V(3).Infof("Turbo PolicyBindings: %v.", spewConfigState.Sdump(policyBindings))
}
