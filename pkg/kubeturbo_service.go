package kubeturbo

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/action"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	"github.com/golang/glog"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
	"github.com/vmturbo/kubeturbo/pkg/turbostore"
)

type KubeturboService struct {
	config *Config

	// Turbonomic scheduler
	TurboScheduler *turboscheduler.TurboScheduler
	actionHandler  *action.ActionHandler

	k8sTAPService *K8sTAPService
}

func NewKubeturboService(c *Config) *KubeturboService {
	turboScheduler := turboscheduler.NewTurboScheduler(c.Client, c.tapSpec.TurboServer,
		c.tapSpec.OpsManagerUsername, c.tapSpec.OpsManagerPassword)

	// Create action handler.
	actionHandlerConfig := action.NewActionHandlerConfig(c.Client, c.broker)
	actionHandler := action.NewActionHandler(actionHandlerConfig, turboScheduler)

	k8sTAPServiceConfig := NewK8sTAPServiceConfig(c.Client, c.ProbeConfig, c.tapSpec)

	k8sTAPService, err := NewKubernetesTAPService(k8sTAPServiceConfig, actionHandler)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kuberntes TAP service: %s", err)
	}
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

	var uid string
	parentRefObject, _ := probe.FindParentReferenceObject(pod)
	if parentRefObject != nil {
		uid = string(parentRefObject.UID)
	} else {
		uid = string(pod.UID)
	}
	producer := &turbostore.PodProducer{}
	err = producer.Produce(v.config.broker, uid, pod)
	if err != nil {
		glog.Errorf("Got error when producing pod: %s", err)
		v.regularSchedulePod(pod)
	}
}

func (v *KubeturboService) regularSchedulePod(pod *api.Pod) {
	if err := v.TurboScheduler.Schedule(pod); err != nil {
		glog.Errorf("Scheduling failed: %s", err)
	}
}

//
//// Find Action related to pod in action event repository.
//func (v *KubeturboService) actionPodFilter(pod *api.Pod) bool {
//	var uid string
//	parentRefObject, _ := probe.FindParentReferenceObject(pod)
//	if parentRefObject != nil {
//		uid = string(parentRefObject.UID)
//	} else {
//		uid = string(pod.UID)
//	}
//	_, exist := v.config.turboStore.Get(uid)
//	return exist
//}
