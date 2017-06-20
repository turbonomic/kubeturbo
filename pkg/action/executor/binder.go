package executor

import (
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/golang/glog"
)

// Binder is used to schedule pod onto node.
type binder struct {
	*client.Clientset
}

// Bind just does a POST binding RPC.
func (b *binder) Bind(binding *api.Binding) error {
	glog.V(2).Infof("Attempting to bind %v to %v", binding.Name, binding.Target.Name)
	return b.CoreV1().Pods(binding.Namespace).Bind(binding)
}
