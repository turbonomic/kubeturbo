package kubeturbo

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/action"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	"github.com/golang/glog"
)

type KubeturboService struct {
	config *Config

	// Turbonomic scheduler
	TurboScheduler *turboscheduler.TurboScheduler
	actionHandler  *action.ActionHandler

	k8sTAPService *K8sTAPService
}

func NewKubeturboService(c *Config) *KubeturboService {
	k8sTAPServiceConfig := NewK8sTAPServiceConfig(c.Client, c.ProbeConfig, c.tapSpec)

	k8sTAPService, err := NewKubernetesTAPService(k8sTAPServiceConfig)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kuberntes TAP service: %s", err)
	}

	turboScheduler := turboscheduler.NewTurboScheduler(c.Client, c.tapSpec.TurboServer,
		c.tapSpec.OpsManagerUsername, c.tapSpec.OpsManagerPassword)

	// Create action handler.
	actionHandlerConfig := action.NewActionHandlerConfig(c.Client)
	actionHandler := action.NewActionHandler(actionHandlerConfig, turboScheduler)

	return &KubeturboService{
		config:         c,
		TurboScheduler: turboScheduler,
		actionHandler:  actionHandler,

		k8sTAPService: k8sTAPService,
	}
}

// Run begins watching and scheduling. It starts a goroutine and returns immediately.
func (v *KubeturboService) Run() {
	glog.V(2).Infof("********** Start runnning Kubeturbo Service **********")

	// These three go routine is responsible for watching corresponding watchable resource.
	//go wait.Until(v.getNextNode, 0, v.config.StopEverything)
	go wait.Until(v.getNextPod, 0, v.config.StopEverything)
	go v.k8sTAPService.ConnectToTurbo()

}

func (v *KubeturboService) getNextPod() {
	p, err := v.config.PodQueue.Pop(nil)
	if err != nil {
		// TODO
	}
	pod := p.(*api.Pod)
	glog.V(3).Infof("Get a new Pod %v", pod.Name)

	if v.actionHandler.CheckPodAction(pod) {
		return
	}

	v.regularSchedulePod(pod)
}

func (v *KubeturboService) regularSchedulePod(pod *api.Pod) {
	if err := v.TurboScheduler.Schedule(pod); err != nil {
		glog.Errorf("Scheduling failed: %s", err)
	}
}
