package vmtscheduler

import (
	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
	"github.com/vmturbo/kubeturbo/pkg/scheduler/vmtscheduler/reservation"

	// "github.com/golang/glog"
)

type Config struct {
	Meta *vmtmeta.VMTMeta
}

type VMTScheduler struct {
	config *Config
}

func NewVMTScheduler(kubeClient *client.Client, meta *vmtmeta.VMTMeta) *VMTScheduler {
	config := &Config{
		Meta: meta,
	}

	return &VMTScheduler{
		config: config,
	}
}

// use vmt api to get reservation destinations
// TODO for now only deal with one pod at a time
// But the result is a map. Will change later when deploy works.
func (s *VMTScheduler) GetDestinationFromVmturbo(pod *api.Pod) (map[*api.Pod]string, error) {
	deployRequest := reservation.NewDeployment(s.config.Meta)

	// reservationResult is map[string]string -- [podName]nodeName
	// TODO !!!!!!! Now only support a single pod.
	return deployRequest.GetDestinationFromVmturbo(pod)
}
