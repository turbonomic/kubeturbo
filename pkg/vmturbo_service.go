package kubeturbo

import (
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"
	comm "github.com/vmturbo/kubeturbo/pkg/vmturbocommunicator"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

type VMTurboService struct {
	config       *Config
	vmtcomm      *comm.VMTCommunicator
	vmtEventChan chan *registry.VMTEvent
	// VMTurbo Scheduler
	TurboScheduler *turboscheduler.TurboScheduler
}

func NewVMTurboService(c *Config) *VMTurboService {
	turboSched := turboscheduler.NewTurboScheduler(c.Client, c.Meta)

	vmtEventChannel := make(chan *registry.VMTEvent)

	vmtCommunicator := comm.NewVMTCommunicator(c.Client, c.Meta, c.EtcdStorage)

	return &VMTurboService{
		config:         c,
		vmtcomm:        vmtCommunicator,
		vmtEventChan:   vmtEventChannel,
		TurboScheduler: turboSched,
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
	glog.V(2).Infof("Get a new VMTEvent from etcd: %v", event)
	if event.ActionType == "move" || event.ActionType == "provision" {
		glog.V(2).Infof("VMTEvent must be handled.")
		// Send VMTEvent to channel.
		v.vmtEventChan <- event
	}
}

// When a new node is added in, this function is called. Otherwise, it is blocked.
func (v *VMTurboService) getNextNode() {
	node := v.config.NodeQueue.Pop().(*api.Node)
	glog.V(3).Infof("Get a new Node %v", node.Name)
}

// Whenever there is a new pod created and post to etcd, this method will be used to deal with
// unhandled pod. Otherwise it will block at Pop()
func (v *VMTurboService) getNextPod() {
	pod := v.config.PodQueue.Pop().(*api.Pod)
	glog.V(3).Infof("Get a new Pod %v", pod.Name)

	// If we want to track new Pod, create a VMTEvent and post it to etcd.
	vmtEvents := registry.NewVMTEvents(v.config.Client, "", v.config.EtcdStorage)
	event := registry.GenerateVMTEvent("create", pod.Namespace, pod.Name, "", 1)
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	select {
	case vmtEventFromEtcd := <-v.vmtEventChan:
		glog.V(4).Infof("Receive VMTEvent")

		hasError := false
		switch vmtEventFromEtcd.ActionType {
		case "move":

			if validatePodToBeMoved(pod, vmtEventFromEtcd) {
				glog.V(2).Infof("Pod %s/%s is to be scheduled to %s as a result of MOVE action",
					pod.Namespace, pod.Name, vmtEventFromEtcd.Destination)

				v.TurboScheduler.ScheduleTo(pod, vmtEventFromEtcd.Destination)
			} else {
				hasError = true
			}

			break
		case "provision":
			glog.V(3).Infof("Change replicas of %s.", vmtEventFromEtcd.TargetSE)

			// double check if the pod is created as the result of provision
			hasPrefix := strings.HasPrefix(pod.Name, vmtEventFromEtcd.TargetSE)
			if !hasPrefix {
				hasError = true
				break
			}
			err := v.TurboScheduler.Schedule(pod)
			if err != nil {
				hasError = true
				glog.Errorf("Scheduling failed: %s", err)
			}
		}

		if hasError {
			// Send back action failed. Then simply deploy the pod using scheduler.
			glog.V(2).Infof("Action failed")
			if vmtEventFromEtcd.VMTMessageID > -1 {
				v.vmtcomm.SendActionReponse(sdk.ActionResponseState_FAILED, int32(0), int32(vmtEventFromEtcd.VMTMessageID), "Failed")
			}
			break
		} else {
			// TODO, at this point, we really do not know if the assignment of the pod succeeds or not.
			// The right place of sending move reponse is after the event.
			// Here for test purpose, send the move success action response.
			if vmtEventFromEtcd.VMTMessageID > -1 {
				glog.V(2).Infof("Action Succeeded.")
				progress := int32(100)
				v.vmtcomm.SendActionReponse(sdk.ActionResponseState_SUCCEEDED, progress, int32(vmtEventFromEtcd.VMTMessageID), "Success")
			}
			time.Sleep(time.Millisecond * 500)
			v.vmtcomm.DiscoverTarget()
		}
		return
	default:
		glog.V(3).Infof("No VMTEvent from ETCD. Simply schedule the pod.")
	}
	err := v.TurboScheduler.Schedule(pod)

	if err != nil {
		glog.Errorf("Scheduling failed: %s", err)
	}
}

func validatePodToBeMoved(pod *api.Pod, vmtEvent *registry.VMTEvent) bool {
	// TODO. Now based on name.
	eventPodNamespace := vmtEvent.Namespace
	eventPodName := vmtEvent.TargetSE
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
