package processor

import (
	"fmt"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

type TurboPolicyProcessor struct {
	ClusterScraper cluster.ClusterScraperInterface
	KubeCluster    *repository.KubeCluster
}

func NewTurboPolicyProcessor(clusterScraper cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *TurboPolicyProcessor {
	return &TurboPolicyProcessor{
		ClusterScraper: clusterScraper,
		KubeCluster:    kubeCluster,
	}
}

func (p *TurboPolicyProcessor) ProcessTurboPolicies() {
	turboSloScalings, err := p.ClusterScraper.GetAllTurboSLOScalings()
	if err != nil {
		glog.Warningf("Failed to list SLOHorizontalScales: %v.", err)
		return
	}
	if len(turboSloScalings) == 0 {
		glog.V(2).Info("There is no SLOHorizontalScale resource found in the cluster.")
		return
	}
	glog.V(2).Infof("Discovered %v SLOHorizontalScale policies.", len(turboSloScalings))

	turboPolicyBindings, err := p.ClusterScraper.GetAllTurboPolicyBindings()
	if err != nil {
		glog.Warningf("Failed to list PolicyBindings: %v.", err)
		return
	}
	if len(turboPolicyBindings) == 0 {
		glog.V(2).Info("There is no PolicyBinding resource found in the cluster.")
		return
	}
	glog.V(2).Infof("Discovered %v PolicyBindings.", len(turboPolicyBindings))

	policyMap := make(map[string]*repository.TurboPolicy)
	for _, sloScale := range turboSloScalings {
		turboSloScaling := sloScale
		gvk := turboSloScaling.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			continue
		}
		policyId := createPolicyId(gvk.Kind, turboSloScaling.GetNamespace(), turboSloScaling.GetName())
		policyMap[policyId] = repository.
			NewTurboPolicy().
			WithSLOHorizontalScale(&turboSloScaling)
	}

	var policyBindings []*repository.TurboPolicyBinding
	for _, policyBinding := range turboPolicyBindings {
		turboPolicyBinding := policyBinding
		targets := turboPolicyBinding.Spec.Targets
		if len(targets) == 0 {
			glog.Warningf("PolicyBinding %v/%v has no targets defined. Skip.",
				turboPolicyBinding.Namespace, turboPolicyBinding.Name)
			continue
		}
		policyRef := turboPolicyBinding.Spec.PolicyRef
		policyId := createPolicyId(policyRef.Kind, turboPolicyBinding.GetNamespace(), policyRef.Name)
		if policy, found := policyMap[policyId]; found {
			policyBindings = append(policyBindings, repository.
				NewTurboPolicyBinding(&turboPolicyBinding).
				WithTurboPolicy(policy))
		} else {
			glog.Warningf("PolicyBinding %v/%v refers to %v policy %v/%v which does not exist. Skip.",
				turboPolicyBinding.Namespace, turboPolicyBinding.Name, policyRef.Kind,
				turboPolicyBinding.Namespace, policyRef.Name)
		}
	}
	glog.V(2).Infof("Discovered %v valid PolicyBindings.", len(policyBindings))
	p.KubeCluster.TurboPolicyBindings = policyBindings
}

func createPolicyId(kind, namespace, name string) string {
	return fmt.Sprintf("%v-%v/%v", kind, namespace, name)
}
