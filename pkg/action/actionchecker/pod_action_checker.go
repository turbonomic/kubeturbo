package actionchecker

import (
	"github.com/vmturbo/kubeturbo/pkg/turbostore"
)

type PodActionChecker struct {
	repo *turbostore.TurboStore
}

func NewPodActionChecker(repo *turbostore.TurboStore) *PodActionChecker {
	return &PodActionChecker{
		repo: repo,
	}
}
//
//// Find Action related to pod in action event repository.
//func (c *PodActionChecker) FindAction(pod *api.Pod) (*turboaction.TurboAction, error) {
//	var kind, name string
//	parentRefObject, _ := probe.FindParentReferenceObject(pod)
//	if parentRefObject != nil {
//		kind = parentRefObject.Kind
//		name = parentRefObject.Name
//	} else {
//		kind = pod.Kind
//		name = pod.Name
//	}
//	return c.repo.GetAction(kind, name)
//}
