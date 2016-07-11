package kubeturbo

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"
	comm "github.com/vmturbo/kubeturbo/pkg/vmturbocommunicator"

	"github.com/golang/glog"
)

type VMTurboService struct {
	config           *Config
	vmtEventRegistry *registry.VMTEventRegistry
	vmtcomm          *comm.VMTCommunicator
	vmtEventChan     chan *registry.VMTEvent
	// VMTurbo Scheduler
	TurboScheduler *turboscheduler.TurboScheduler
}

func NewVMTurboService(c *Config) *VMTurboService {
	turboSched := turboscheduler.NewTurboScheduler(c.Client, c.Meta)

	vmtEventChannel := make(chan *registry.VMTEvent)
	vmtEventRegistry := registry.NewVMTEventRegistry(c.EtcdStorage)
	vmtCommunicator := comm.NewVMTCommunicator(c.Client, c.Meta, c.EtcdStorage, c.ProbeConfig)

	return &VMTurboService{
		config:           c,
		vmtEventRegistry: vmtEventRegistry,
		vmtcomm:          vmtCommunicator,
		vmtEventChan:     vmtEventChannel,
		TurboScheduler:   turboSched,
	}
}

// Run begins watching and scheduling. It starts a goroutine and returns immediately.
func (v *VMTurboService) Run() {
	glog.V(2).Infof("********** Start runnning VMT service **********")

	// register and validates to vmturbo server
	go v.vmtcomm.Run()

	// These three go routine is responsible for watching corresponding watchable resource.
	go wait.Until(v.getNextNode, 0, v.config.StopEverything)
	go wait.Until(v.getNextPod, 0, v.config.StopEverything)
	go wait.Until(v.getNextVMTEvent, 0, v.config.StopEverything)
}

func (v *VMTurboService) getNextVMTEvent() {
	event := v.config.VMTEventQueue.Pop().(*registry.VMTEvent)
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

// When a new node is added in, this function is called. Otherwise, it is blocked.
func (v *VMTurboService) getNextNode() {
	node := v.config.NodeQueue.Pop().(*api.Node)
	glog.V(3).Infof("Get a new Node %v", node.Name)
}

func (v *VMTurboService) getNextPod() {
	pod := v.config.PodQueue.Pop().(*api.Pod)
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

func (v *VMTurboService) regularSchedulePod(pod *api.Pod) {
	if err := v.TurboScheduler.Schedule(pod); err != nil {
		glog.Errorf("Scheduling failed: %s", err)
	}
}

// If the event has not expired (within 10s since it is created), then update
// the event and put it back to queue. Otherwise return an error.
func (v *VMTurboService) reprocessEvent(event *registry.VMTEvent) error {
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
