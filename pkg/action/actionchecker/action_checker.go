package actionchecker

import (
	"k8s.io/kubernetes/pkg/runtime"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
)

type ActionChecker interface {
	// check if an Object is created as a result of action.
	// If true, return the action. Otherwise, return nil or err.
	FindAction(obj runtime.Object) (*turboaction.TurboAction, error)
}
