package supervisor

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/util/wait"

	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"

	"errors"
	"github.com/golang/glog"
)

type EventCheckFunc func(event *turboaction.TurboAction) (bool, error)

type ActionSupervisorConfig struct {
	kubeClient *client.Client

	// The following three channels are used between action handler and action supervisor to pass action information.
	executedActionChan  chan *turboaction.TurboAction
	succeededActionChan chan *turboaction.TurboAction
	failedActionChan    chan *turboaction.TurboAction

	StopEverything chan struct{}
}

func NewActionSupervisorConfig(kubeClient *client.Client, exeChan, succChan, failedChan chan *turboaction.TurboAction) *ActionSupervisorConfig {
	config := &ActionSupervisorConfig{
		kubeClient: kubeClient,

		executedActionChan:  exeChan,
		succeededActionChan: succChan,
		failedActionChan:    failedChan,

		StopEverything: make(chan struct{}),
	}

	return config

}

// ActionSupervisor verifies if an executed action succeed or failed.
type ActionSupervisor struct {
	config *ActionSupervisorConfig
}

func NewActionSupervisor(config *ActionSupervisorConfig) *ActionSupervisor {

	return &ActionSupervisor{
		config: config,
	}
}

func (s *ActionSupervisor) Start() {
	go wait.Until(s.getNextVMTEvent, 0, s.config.StopEverything)

}

func (s *ActionSupervisor) getNextVMTEvent() {
	event := <-s.config.executedActionChan
	glog.V(3).Infof("Executed event is %v", event)
	// TODO use agent to verify if the event succeeds
	switch {
	case event.Content.ActionType == "move":
		s.updateVMTEvent(event, s.checkMoveAction)
	case event.Content.ActionType == "provision":
		s.updateVMTEvent(event, s.checkProvisionAction)
	case event.Content.ActionType == "unbind":
		s.updateVMTEvent(event, s.checkUnbindAction)
	}
}

func (s *ActionSupervisor) checkMoveAction(event *turboaction.TurboAction) (bool, error) {
	glog.Infof("Now check move action")
	if event.Content.ActionType != "move" {
		glog.Infof("Not a move")
		return false, errors.New("Not a move action")
	}
	moveSpec := event.Content.ActionSpec.(turboaction.MoveSpec)
	podName := moveSpec.NewObjectName
	podNamespace := event.Namespace
	podIdentifier := podNamespace + "/" + podName

	targetPod, err := s.config.kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		glog.Infof("Cannot find pod")
		return false, fmt.Errorf("Cannot find pod %s in cluster after a move action", podIdentifier)
	}
	moveDestination := moveSpec.Destination
	actualHostingNode := targetPod.Spec.NodeName
	if actualHostingNode == moveDestination {
		glog.Infof("Move action succeeded.")
		return true, nil
	}
	glog.Errorf("Move action failed. Incorrect move destination %s", actualHostingNode)
	return false, nil
}

func (s *ActionSupervisor) checkProvisionAction(event *turboaction.TurboAction) (bool, error) {
	if event.Content.ActionType != "provision" {
		return false, errors.New("Not a provision action")
	}
	return s.checkScaleAction(event)
}

func (s *ActionSupervisor) checkUnbindAction(event *turboaction.TurboAction) (bool, error) {
	if event.Content.ActionType != "unbind" {
		return false, errors.New("Not a unbind action")
	}
	return s.checkScaleAction(event)
}

func (s *ActionSupervisor) checkScaleAction(event *turboaction.TurboAction) (bool, error) {
	name := event.Content.ParentObjectRef.ParentObjectName
	namespace := event.Namespace
	identifier := namespace + "/" + name

	targetRC, _ := s.config.kubeClient.ReplicationControllers(namespace).Get(name)
	targetDeployment, _ := s.config.kubeClient.Deployments(namespace).Get(name)

	if (targetDeployment == nil || targetDeployment.Name == "") && (targetRC == nil || targetRC.Name == "") {
		return false, fmt.Errorf("Cannot find replication controller or deployment %s in cluster", identifier)
	}

	scaleSpec := event.Content.ActionSpec.(turboaction.ScaleSpec)
	targetReplicas := scaleSpec.NewReplicas
	currentReplicas := int32(0)
	if targetRC != nil && targetRC.Name != "" {
		currentReplicas = targetRC.Spec.Replicas
		glog.V(4).Infof("currentReplicas from RC is %d", currentReplicas)
	} else if targetDeployment != nil && targetDeployment.Name != "" {
		currentReplicas = targetDeployment.Spec.Replicas
		glog.V(4).Infof("currentReplicas from Deployment is %d", currentReplicas)
	}
	glog.V(4).Infof("replica want %d, is %d", targetReplicas, currentReplicas)
	if targetReplicas == currentReplicas {
		return true, nil
	}
	return false, nil
}

func (s *ActionSupervisor) updateVMTEvent(event *turboaction.TurboAction, checkFunc EventCheckFunc) {
	successful, err := checkFunc(event)
	if err != nil {
		glog.Errorf("Error checking action: %s", err)
		return
	}
	if successful {
		glog.Infof("Failed")
		s.config.succeededActionChan <- event
		return
	}
	// Check if the event has expired. If true, update the status to fail and return;
	// Otherwise, only update the LastTimestamp.
	expired := checkExpired(event)
	glog.Infof("checkExpired result %v", expired)
	if expired {
		glog.Errorf("Timeout processing %s event on %s-%s",
			event.Content.ActionType, event.Content.TargetObject.TargetObjectType, event.Content.TargetObject.TargetObjectName)
		glog.Infof("------------------------")
		s.config.failedActionChan <- event
		return
	}
	time.Sleep(time.Second * 1)
	glog.Infof("Update timestamp")
	// update timestamp
	event.LastTimestamp = time.Now()

	return
}

func checkExpired(event *turboaction.TurboAction) bool {
	now := time.Now()
	duration := now.Sub(event.LastTimestamp)
	glog.Infof("Duration is %v", duration)
	if duration > time.Duration(10)*time.Second {
		return true
	}
	return false
}
