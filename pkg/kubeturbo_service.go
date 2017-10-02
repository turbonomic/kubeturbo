package kubeturbo

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/wait"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action"
	discutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	turboscheduler "github.com/turbonomic/kubeturbo/pkg/scheduler"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"

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
	turboScheduler := turboscheduler.NewTurboScheduler(c.Client, c.tapSpec.TurboServer,
		c.tapSpec.OpsManagerUsername, c.tapSpec.OpsManagerPassword)

	// Create action handler.
	actionHandlerConfig := action.NewActionHandlerConfig(c.Client, c.KubeletClient, c.k8sVersion, c.noneSchedulerName)
	actionHandler := action.NewActionHandler(actionHandlerConfig)

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
		glog.Errorf("Failed to get the pending pod: %s", err)
		return
	}
	pod := p.(*api.Pod)
	glog.V(3).Infof("Get a new Pod %v", pod.Name)

	var key string
	parentRefObject, _ := discutil.FindParentReferenceObject(pod)
	if parentRefObject != nil {
		key = string(parentRefObject.UID)
	} else {
		key = fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	}
	producer := &turbostore.PodProducer{}
	err = producer.Produce(v.config.broker, key, pod)
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
