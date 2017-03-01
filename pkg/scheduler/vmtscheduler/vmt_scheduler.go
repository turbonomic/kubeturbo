package vmtscheduler

import (
	"k8s.io/kubernetes/pkg/api"
	"github.com/vmturbo/kubeturbo/pkg/scheduler/vmtscheduler/reservation"
	// "github.com/golang/glog"
)

type Config struct {
	// TODO replace with the new API client
	TurboServer        string
	OpsManagerUsername string
	OpsManagerPassword string
}

type VMTScheduler struct {
	config *Config
}

func NewVMTScheduler(serverURL, username, password string) *VMTScheduler {
	config := &Config{
		TurboServer:        serverURL,
		OpsManagerUsername: username,
		OpsManagerPassword: password,
	}

	return &VMTScheduler{
		config: config,
	}
}

// use vmt api to get reservation destinations
// TODO for now only deal with one pod at a time
// But the result is a map. Will change later when deploy works.
func (s *VMTScheduler) GetDestinationFromVmturbo(pod *api.Pod) (map[*api.Pod]string, error) {
	deployRequest := reservation.NewDeployment(s.config.TurboServer, s.config.OpsManagerUsername, s.config.OpsManagerPassword)

	// reservationResult is map[string]string -- [podName]nodeName
	// TODO !!!!!!! Now only support a single pod.
	return deployRequest.GetDestinationFromVmturbo(pod)
}
