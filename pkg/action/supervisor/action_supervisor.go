package supervisor

import (
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"

	"github.com/golang/glog"
)

const (
	//TODO: set timeout for each Action
	defaultTimeOut = time.Duration(30) * time.Second
)

type CheckActionFunc func(event *turboaction.TurboAction) (bool, error)

type ActionSupervisorConfig struct {
	kubeClient *client.Clientset

	// The following three channels are used between action handler and action supervisor to pass action information.
	executedActionChan  chan *turboaction.TurboAction
	succeededActionChan chan *turboaction.TurboAction
	failedActionChan    chan *turboaction.TurboAction

	StopEverything chan struct{}
}

func NewActionSupervisorConfig(kubeClient *client.Clientset, exeChan, succChan,
	failedChan chan *turboaction.TurboAction) *ActionSupervisorConfig {
	config := &ActionSupervisorConfig{
		kubeClient: kubeClient,

		executedActionChan:  exeChan,
		succeededActionChan: succChan,
		failedActionChan:    failedChan,

		StopEverything: make(chan struct{}),
	}

	return config

}

// Action supervisor verifies if an executed action succeeds or fails.
type ActionSupervisor struct {
	config *ActionSupervisorConfig
}

func NewActionSupervisor(config *ActionSupervisorConfig) *ActionSupervisor {

	return &ActionSupervisor{
		config: config,
	}
}

func (s *ActionSupervisor) Start() {
	go wait.Until(s.getNextExecutedTurboAction, 0, s.config.StopEverything)
}

func (s *ActionSupervisor) getNextExecutedTurboAction() {
	action := <-s.config.executedActionChan
	glog.V(3).Infof("Executed action is %v", action)
	switch {
	case action.Content.ActionType == "move":
		s.updateAction(action, s.checkMoveAction)
	case action.Content.ActionType == "provision":
		s.updateAction(action, s.checkProvisionAction)
	case action.Content.ActionType == "unbind":
		s.updateAction(action, s.checkUnbindAction)
	}
}

func (s *ActionSupervisor) checkMoveAction(action *turboaction.TurboAction) (bool, error) {
	glog.V(2).Infof("Checking a move action")
	if action.Content.ActionType != "move" {
		glog.Error("Not a move action")
		return false, errors.New("Not a move action")
	}
	moveSpec := action.Content.ActionSpec.(turboaction.MoveSpec)
	podName := moveSpec.NewObjectName
	podNamespace := action.Namespace
	podIdentifier := util.BuildIdentifier(podNamespace, podName)

	targetPod, err := s.config.kubeClient.CoreV1().Pods(podNamespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("move-check failed: cannot find pod %v: %v", podIdentifier, err.Error())
		glog.Error(err.Error())
		return false, err
	}

	phase := targetPod.Status.Phase
	if phase == api.PodRunning || (phase == api.PodPending && targetPod.DeletionGracePeriodSeconds == nil) {
		moveDestination := moveSpec.Destination
		actualHostingNode := targetPod.Spec.NodeName
		if actualHostingNode == moveDestination {
			glog.V(3).Infof("Move pod [%s/%s] action succeeded.", podNamespace, podName)
			return true, nil
		} else {
			glog.Errorf("Move action failed. Incorrect move destination %s", actualHostingNode)
			return false, nil
		}
	}

	err = fmt.Errorf("move-check failed: new pod status is %v", targetPod.Status.Phase)
	glog.Error(err.Error())
	return false, err
}

func (s *ActionSupervisor) checkProvisionAction(event *turboaction.TurboAction) (bool, error) {
	glog.V(2).Infof("Checking a provision action.")
	if event.Content.ActionType != "provision" {
		return false, errors.New("Not a provision action")
	}
	return s.checkScaleAction(event)
}

func (s *ActionSupervisor) checkUnbindAction(event *turboaction.TurboAction) (bool, error) {
	glog.V(2).Infof("Checking an unbind action")
	if event.Content.ActionType != "unbind" {
		return false, errors.New("Not a unbind action")
	}
	return s.checkScaleAction(event)
}

func (s *ActionSupervisor) checkScaleAction(action *turboaction.TurboAction) (bool, error) {
	name := action.Content.ParentObjectRef.ParentObjectName
	namespace := action.Namespace
	identifier := util.BuildIdentifier(namespace, name)

	var currentReplicas int32
	switch action.Content.ParentObjectRef.ParentObjectType {
	case turboaction.TypeReplicationController:
		targetRC, err := s.config.kubeClient.CoreV1().ReplicationControllers(namespace).Get(name, metav1.GetOptions{})
		if err != nil || targetRC == nil {
			return false, fmt.Errorf("Cannot find replication controller for %s in cluster", identifier)
		}
		currentReplicas = *targetRC.Spec.Replicas
		glog.V(4).Infof("currentReplicas from RC is %d", currentReplicas)
		break
	case turboaction.TypeReplicaSet:
		targetReplicaSet, err := s.config.kubeClient.ExtensionsV1beta1().ReplicaSets(namespace).Get(name, metav1.GetOptions{})
		if err != nil || targetReplicaSet == nil {
			return false, fmt.Errorf("Cannot find replica set for %s in cluster", identifier)
		}
		currentReplicas = *targetReplicaSet.Spec.Replicas
		glog.V(4).Infof("current replica of target replica set is %d", currentReplicas)
		break
	}

	scaleSpec := action.Content.ActionSpec.(turboaction.ScaleSpec)
	targetReplicas := scaleSpec.NewReplicas
	glog.V(4).Infof("replica wanted is %d, current replica is %d", targetReplicas, currentReplicas)
	if targetReplicas == currentReplicas {
		return true, nil
	}
	return false, nil
}

func (s *ActionSupervisor) updateAction(action *turboaction.TurboAction, checkFunc CheckActionFunc) {
	// Check if the event has expired. If true, update the status to fail and return;
	// Otherwise, only update the LastTimestamp.
	for !checkExpired(action) {
		successful, err := checkFunc(action)
		if err != nil {
			// TODO: do we want to return?
			glog.Errorf("Error checking action: %v", err)
		}
		if successful {
			action.Status = turboaction.Success
			action.LastTimestamp = time.Now()
			s.config.succeededActionChan <- action
			return
		}

		time.Sleep(time.Second * 1)
	}
	glog.Errorf("Timeout processing when %s action on %s-%s", action.Content.ActionType,
		action.Content.TargetObject.TargetObjectType, action.Content.TargetObject.TargetObjectName)
	action.Status = turboaction.Fail
	action.LastTimestamp = time.Now()
	s.config.failedActionChan <- action
	return
}

// check whether it is timeOut since the action was executed.
// the action.LastTimestamp is set by the action executor when the action has been executed.
func checkExpired(action *turboaction.TurboAction) bool {
	now := time.Now()
	duration := now.Sub(action.LastTimestamp)
	glog.V(3).Infof("Duration is %v", duration)
	if duration > defaultTimeOut {
		return true
	}
	return false
}
