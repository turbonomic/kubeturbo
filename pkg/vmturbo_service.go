package kubeturbo

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/discovery"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	"github.com/golang/glog"
)

type KubeturboService struct {
	config *Config

	vmtEventRegistry *registry.VMTEventRegistry
	vmtEventChan     chan *registry.VMTEvent
	// Turbonomic scheduler
	TurboScheduler *turboscheduler.TurboScheduler

	// TAP Service
	k8sTapService *K8sTAPService
}

func NewKubeturboService(c *Config) *KubeturboService {

	probeCategory := "CloudNative"
	targetType := "Kubernetes"
	turboCommConf := "cmd/kubeturbo/container-conf.json"
	targetID := c.Meta.TargetIdentifier

	targetConfig := discovery.NewK8sTargetConfig(c.Meta.TargetIdentifier, c.Meta.Username, c.Meta.Password)

	discoveryConfig := discovery.NewDiscoveryConfig(c.Client, c.ProbeConfig, targetConfig)

	k8sTAPServiceConfig := NewK8sTAPServiceConfig(turboCommConf, probeCategory,
		targetType, targetID, discoveryConfig)

	k8sTAPService, err := NewKubernetesTAPService(k8sTAPServiceConfig)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kuberntes TAP service: %s", err)
	}

	turboScheduler := turboscheduler.NewTurboScheduler(c.Client, c.Meta)
	vmtEventChannel := make(chan *registry.VMTEvent)
	vmtEventRegistry := registry.NewVMTEventRegistry(c.EtcdStorage)

	return &KubeturboService{
		config:           c,
		vmtEventRegistry: vmtEventRegistry,
		vmtEventChan:     vmtEventChannel,
		TurboScheduler:   turboScheduler,

		k8sTapService: k8sTAPService,
	}
}

// Run begins watching and scheduling. It starts a goroutine and returns immediately.
func (v *KubeturboService) Run() {
	glog.V(2).Infof("********** Start runnning Kubeturbo Service **********")

	// These three go routine is responsible for watching corresponding watchable resource.
	go wait.Until(v.getNextPod, 0, v.config.StopEverything)
	go wait.Until(v.getNextVMTEvent, 0, v.config.StopEverything)

	go v.k8sTapService.ConnectToTurbo()
}

func (v *KubeturboService) getNextVMTEvent() {
	e, err := v.config.VMTEventQueue.Pop(nil)
	if err != nil {
		// TODO
		glog.Errorf("Error pop from event queue: %v", err)
	}
	event := e.(*registry.VMTEvent)
	glog.V(2).Infof("Get a new pending VMTEvent from etcd: %v", event)
	content := event.Content
	if content.ActionType == "move" || content.ActionType == "provision" {
		glog.V(2).Infof("VMTEvent %s on %s must be handled.", content.ActionType, content.TargetSE)
		// Send VMTEvent to channel.
		v.vmtEventChan <- event
	} else if content.ActionType == "unbind" {
		glog.V(3).Infof("Decrease the replicas of %s.", content.TargetSE)

		if err := v.vmtEventRegistry.UpdateStatus(event, registry.Executed); err != nil {
			glog.Errorf("Failed to update event status: %s", err)
		}
	}
}

func (v *KubeturboService) getNextPod() {
	p, err := v.config.PodQueue.Pop(nil)
	if err != nil {
		// TODO
	}
	pod := p.(*api.Pod)
	glog.V(3).Infof("Get a new Pod %v", pod.Name)

	select {
	case vmtEventFromEtcd := <-v.vmtEventChan:
		content := vmtEventFromEtcd.Content
		glog.V(3).Infof("Receive VMTEvent type %s", content.ActionType)

		var actionExecutionError error
		switch content.ActionType {
		case "move":
			if validatePodToBeMoved(pod, vmtEventFromEtcd) {
				glog.V(2).Infof("Pod %s/%s is to be scheduled to %s as a result of MOVE action",
					pod.Namespace, pod.Name, content.MoveSpec.Destination)

				// Since here new pod is created as a result of move action, it needs to change
				// the TargetSE in VMTEvent content.
				vmtEventFromEtcd.Content.TargetSE = pod.Name
				v.TurboScheduler.ScheduleTo(pod, content.MoveSpec.Destination)
			} else {
				time.Sleep(time.Second * 1)
				actionExecutionError = v.reprocessEvent(vmtEventFromEtcd)
				// pod is not created as a result of move, schedule it regularly
				v.regularSchedulePod(pod)
			}
			break
		case "provision":
			glog.V(3).Infof("Increase the replicas of %s.", content.TargetSE)

			// double check if the pod is created as the result of provision
			hasPrefix := strings.HasPrefix(pod.Name, content.TargetSE)
			if !hasPrefix {
				actionExecutionError = v.reprocessEvent(vmtEventFromEtcd)
				// pod is not created as a result of provision, schedule it regularly
				v.regularSchedulePod(pod)
				break
			}
			err := v.TurboScheduler.Schedule(pod)
			if err != nil {
				glog.Errorf("Scheduling failed: %s", err)
				actionExecutionError = fmt.Errorf("Error scheduling the new provisioned pod: %s", err)
			}
			break
		}

		if actionExecutionError != nil {
			if err := v.vmtEventRegistry.UpdateStatus(vmtEventFromEtcd, registry.Fail); err != nil {
				glog.Errorf("Failed to update event status: %s", err)
			}
			break
		} else {
			if err := v.vmtEventRegistry.UpdateStatus(vmtEventFromEtcd, registry.Executed); err != nil {
				glog.Errorf("Failed to update event status: %s", err)
			}
			return
		}
	default:
		glog.V(3).Infof("No VMTEvent from ETCD. Simply schedule the pod.")
	}

	v.regularSchedulePod(pod)
}

func (v *KubeturboService) regularSchedulePod(pod *api.Pod) {
	if err := v.TurboScheduler.Schedule(pod); err != nil {
		glog.Errorf("Scheduling failed: %s", err)
	}
}

// If the event has not expired (within 10s since it is created), then update
// the event and put it back to queue. Otherwise return an error.
func (v *KubeturboService) reprocessEvent(event *registry.VMTEvent) error {
	now := time.Now()
	duration := now.Sub(event.LastTimestamp)
	if duration > time.Duration(10)*time.Second {
		return fmt.Errorf("Timeout processing %s event on %s",
			event.Content.ActionType, event.Content.TargetSE)
	} else {
		if err := v.vmtEventRegistry.UpdateTimestamp(event); err != nil {
			glog.Errorf("Failed to update timestamp of event %s", event.Name, err)
		}
	}
	return nil
}

func validatePodToBeMoved(pod *api.Pod, vmtEvent *registry.VMTEvent) bool {
	// TODO. Now based on name.
	eventPodNamespace := vmtEvent.Namespace
	eventPodName := vmtEvent.Content.TargetSE
	validEventPodName := eventPodName
	eventPodNamePartials := strings.Split(eventPodName, "-")
	if len(eventPodNamePartials) > 1 {
		validEventPodName = eventPodNamePartials[0]
	}
	eventPodPrefix := eventPodNamespace + "/" + validEventPodName

	podNamespace := pod.Namespace
	podName := pod.Name
	validPodName := podName
	podNamePartials := strings.Split(podName, "-")
	if len(podNamePartials) > 1 {
		validPodName = podNamePartials[0]
	}
	podPrefix := podNamespace + "/" + validPodName

	if eventPodPrefix == podPrefix {
		return true
	} else {
		glog.Warningf("Not the correct pod to be moved. Want to move %s/%s, but get %s/%s."+
			"Now just to schedule it using scheduler.",
			eventPodNamespace, eventPodName, pod.Namespace, pod.Name)
		return false
	}
}
