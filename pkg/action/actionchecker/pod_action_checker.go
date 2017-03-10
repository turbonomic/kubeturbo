package actionchecker

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/vmturbo/kubeturbo/pkg/action/actionrepo"
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
)

type PodActionChecker struct {
	repo *actionrepo.ActionRepository
}

func NewPodActionChecker(repo *actionrepo.ActionRepository) *PodActionChecker {
	return &PodActionChecker{
		repo: repo,
	}
}

// Find Action related to pod in action event repository.
func (c *PodActionChecker) FindAction(pod *api.Pod) (*turboaction.TurboAction, error) {
	var kind, name string
	parentRefObject, _ := probe.FindParentReferenceObject(pod)
	if parentRefObject != nil {
		kind = parentRefObject.Kind
		name = parentRefObject.Name
	} else {
		kind = pod.Kind
		name = pod.Name
	}
	return c.repo.GetAction(kind, name)
}
