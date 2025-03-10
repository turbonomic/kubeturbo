package processor

import (
	"fmt"

	"github.com/golang/glog"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
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
		glog.Warningf("Could not list SLOHorizontalScales: %v.", err)
	}

	turboCvsScalings, err := p.ClusterScraper.GetAllTurboCVSScalings()
	if err != nil {
		glog.Warningf("Could not list ContainerVerticalScales: %v.", err)
	}
	policies := len(turboSloScalings) + len(turboCvsScalings)
	if policies == 0 {
		glog.V(2).Info("There is no SLOHorizontalScale or ContainerVerticalScale resource found in the cluster.")
		return
	}

	glog.V(2).Infof("Discovered %v SLOHorizontalScale policies.", len(turboSloScalings))
	glog.V(2).Infof("Discovered %v ContainerVerticalScale policies.", len(turboCvsScalings))

	turboPolicyBindings, err := p.ClusterScraper.GetAllTurboPolicyBindings()
	if err != nil {
		glog.Warningf("Could not list PolicyBindings: %v.", err)
		return
	}
	if len(turboPolicyBindings) == 0 {
		glog.V(2).Info("There is no PolicyBinding resource found in the cluster.")
		return
	}
	glog.V(2).Infof("Discovered %v PolicyBindings.", len(turboPolicyBindings))

	policyMap := make(map[string]*repository.TurboPolicy)

	for _, sloScale := range turboSloScalings {
		if gvk := sloScale.GetObjectKind().GroupVersionKind(); !gvk.Empty() {
			// Create a copy as sloScale variable is reused during range loop
			sloScaleCopy := sloScale
			policyId := createPolicyId(gvk.Kind, sloScaleCopy.GetNamespace(), sloScaleCopy.GetName())
			policyMap[policyId] = repository.
				NewTurboPolicy().
				WithSLOHorizontalScale(&sloScaleCopy)
		}
	}

	for _, cvsScale := range turboCvsScalings {
		if gvk := cvsScale.GetObjectKind().GroupVersionKind(); !gvk.Empty() {
			// Create a copy as cvsScale variable is reused during range loop
			cvsScaleCopy := cvsScale
			policyId := createPolicyId(gvk.Kind, cvsScaleCopy.GetNamespace(), cvsScaleCopy.GetName())
			policyMap[policyId] = repository.
				NewTurboPolicy().
				WithContainerVerticalScale(&cvsScaleCopy)
		}
	}

	var policyBindings []*repository.TurboPolicyBinding
	for _, policyBinding := range turboPolicyBindings {
		// Create a copy as policyBinding variable is reused during range loop
		policyBindingCopy := policyBinding
		targets := policyBindingCopy.Spec.Targets
		if len(targets) == 0 {
			glog.Warningf("PolicyBinding %v/%v has no targets defined. Skip.",
				policyBindingCopy.Namespace, policyBindingCopy.Name)
			continue
		}
		policyRef := policyBindingCopy.Spec.PolicyRef
		policyId := createPolicyId(policyRef.Kind, policyBindingCopy.GetNamespace(), policyRef.Name)
		if policy, found := policyMap[policyId]; found {
			policyBindings = append(policyBindings, repository.
				NewTurboPolicyBinding(&policyBindingCopy).
				WithTurboPolicy(policy))
		} else {
			glog.Warningf("PolicyBinding %v/%v refers to %v policy %v/%v which does not exist. Skip.",
				policyBindingCopy.Namespace, policyBindingCopy.Name, policyRef.Kind,
				policyBindingCopy.Namespace, policyRef.Name)
		}
	}
	glog.V(2).Infof("Discovered %v valid PolicyBindings.", len(policyBindings))
	p.KubeCluster.TurboPolicyBindings = policyBindings
}

func createPolicyId(kind, namespace, name string) string {
	return fmt.Sprintf("%v-%v/%v", kind, namespace, name)
}
