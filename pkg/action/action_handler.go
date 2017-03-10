package action

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/action/actionchecker"
	"github.com/vmturbo/kubeturbo/pkg/action/actionrepo"
	"github.com/vmturbo/kubeturbo/pkg/action/executor"
	"github.com/vmturbo/kubeturbo/pkg/action/supervisor"
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ActionHandlerConfig struct {
	kubeClient     *client.Client
	StopEverything chan struct{}
}

func NewActionHandlerConfig(kubeClient *client.Client) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		kubeClient: kubeClient,

		StopEverything: make(chan struct{}),
	}

	return config
}

type ActionHandler struct {
	config           *ActionHandlerConfig
	actionExecutor   *executor.ActionExecutor
	actionSupervisor *supervisor.ActionSupervisor

	repo *actionrepo.ActionRepository

	scheduler *turboscheduler.TurboScheduler

	// The following three channels are used between action handler and action supervisor to pass action information.
	// handler -> supervisor
	executedActionChan chan *turboaction.TurboAction
	// supervisor -> handler
	succeededActionChan chan *turboaction.TurboAction
	// supervisor -> handler
	failedActionChan chan *turboaction.TurboAction

	resultChan chan *proto.ActionResult
}

// Build new ActionHandler and start it.
func NewActionHandler(config *ActionHandlerConfig, scheduler *turboscheduler.TurboScheduler) *ActionHandler {
	executedActionChan := make(chan *turboaction.TurboAction, 10)
	succeededActionChan := make(chan *turboaction.TurboAction, 10)
	failedActionChan := make(chan *turboaction.TurboAction, 10)

	supervisorConfig := supervisor.NewActionSupervisorConfig(config.kubeClient, executedActionChan, succeededActionChan, failedActionChan)
	actionSupervisor := supervisor.NewActionSupervisor(supervisorConfig)

	actionExecutor := executor.NewVMTActionExecutor(config.kubeClient)

	handler := &ActionHandler{
		config:           config,
		actionExecutor:   actionExecutor,
		actionSupervisor: actionSupervisor,

		repo: actionrepo.NewActionRepository(),

		scheduler: scheduler,

		executedActionChan:  executedActionChan,
		succeededActionChan: succeededActionChan,
		failedActionChan:    failedActionChan,

		resultChan: make(chan *proto.ActionResult),
	}

	handler.Start()
	return handler
}

// Start watching successful and failed VMTEvents.
// Also start ActionSupervisor to determine the final status of executed VMTEvents.
func (h *ActionHandler) Start() {
	go wait.Until(h.getNextSucceededVMTEvent, 0, h.config.StopEverything)
	go wait.Until(h.getNextFailedVMTEvent, 0, h.config.StopEverything)

	h.actionSupervisor.Start()
}

func (h *ActionHandler) getNextSucceededVMTEvent() {
	event := <-h.succeededActionChan
	glog.V(3).Infof("Succeeded event is %v", event)
	content := event.Content
	msgID := int32(content.ActionMessageID)
	if msgID > -1 {
		glog.V(2).Infof("Action %s for %s-%s succeeded.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
		progress := int32(100)
		h.sendActionResult(proto.ActionResponseState_SUCCEEDED, progress, msgID, "Success")
	}
	// TODO init discovery
}

func (h *ActionHandler) getNextFailedVMTEvent() {
	event := <-h.failedActionChan

	glog.V(3).Infof("Failed event is %v", event)
	content := event.Content
	msgID := int32(content.ActionMessageID)
	if msgID > -1 {
		glog.V(2).Infof("Action %s for %s-%s failed.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
		progress := int32(0)
		h.sendActionResult(proto.ActionResponseState_FAILED, progress, msgID, "Failed 1")
	}
	// TODO init discovery
}

func (h *ActionHandler) Execute(actionItem *proto.ActionItemDTO, msgID int32) {
	actionEvent, err := h.actionExecutor.ExecuteAction(actionItem, msgID)
	if err != nil {
		glog.Errorf("Error execute action: %s", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), msgID, "Failed to execute action")
		return
	}
	isOne, err := isOneStageAction(actionEvent)
	if err != nil {
		glog.Errorf("Error execute action: %s", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), msgID, "Failed to check action")
		return
	}
	if isOne {
		// The action is in Executed stage, then send the action to executedChan
		glog.V(4).Infof("Finished executing action %+v, actionEvent")
		h.executedActionChan <- actionEvent
		return
	}

	//TODO, verify:  If the action is successfully executed and need other steps to change it from Pending to other stage, add it to action repo.
	glog.V(4).Infof("Add %+v to repo", actionEvent)
	h.repo.Add(actionEvent)
}

// When a new pod is created, we need to check if there is an action related to current pod.
// If there is, we need to take over the pod and continue execute the action.
func (h *ActionHandler) CheckPodAction(pod *api.Pod) bool {
	glog.V(4).Infof("Current repo is %+v", h.repo)
	// Check if there is an action in repo related to the pod.
	podActionChecker := actionchecker.NewPodActionChecker(h.repo)
	actionEvent, err := podActionChecker.FindAction(pod)
	if err != nil {
		glog.Infof("Cannot find any action related to Pod %s/%s.", pod.Namespace, pod.Name)
		return false
	}
	go h.HandlePodAction(pod, actionEvent)
	return true
}

// Pod is created as a result of Move or Provision action.
func (h *ActionHandler) HandlePodAction(pod *api.Pod, actionEvent *turboaction.TurboAction) {
	content := actionEvent.Content
	actionType := content.ActionType
	glog.V(3).Infof("Pod %s/%s is related to an %s action", pod.Namespace, pod.Name, actionType)

	var actionExecutionError error
	switch actionType {
	case turboaction.ActionMove:
		moveSpec := content.ActionSpec.(turboaction.MoveSpec)
		glog.V(2).Infof("Pod %s/%s is to be scheduled to %s as a result of MOVE action",
			pod.Namespace, pod.Name, moveSpec.Destination)

		// When rescheduler first create a MOVE action event, it doesn't know what should be the newObjectName,
		// since MOVE replies on replication controller or similar Kubernetes Object to create a new Pod.
		// Then the new pod name becomes the newObjectName.
		moveSpec.NewObjectName = pod.Name
		content.ActionSpec = moveSpec
		actionEvent.Content = content
		err := h.scheduler.ScheduleTo(pod, moveSpec.Destination)
		if err != nil {
			glog.Errorf("Error scheduling the moved pod: %s", err)
			actionExecutionError = fmt.Errorf("Error scheduling the moved pod: %s", err)
		}
		break
	case turboaction.ActionProvision:
		glog.V(3).Infof("Increase the replicas of %s-%s.", content.ParentObjectRef.ParentObjectType, content.ParentObjectRef.ParentObjectName)

		err := h.scheduler.Schedule(pod)
		if err != nil {
			glog.Errorf("Error scheduling the new provisioned pod: %s", err)
			actionExecutionError = fmt.Errorf("Error scheduling the new provisioned pod: %s", err)
		}
		break
	}

	if actionExecutionError != nil {
		//send failed chan
		glog.V(4).Infof("Failed to execute action %+v", actionEvent)
		h.failedActionChan <- actionEvent
		return
	}
	// send executed chan
	glog.V(4).Infof("Finished executing action %+v", actionEvent)
	h.executedActionChan <- actionEvent
}

func (handler *ActionHandler) ResultChan() *proto.ActionResult {
	return <-handler.resultChan
}

// Send action response to Turbonomic server.
func (handler *ActionHandler) sendActionResult(state proto.ActionResponseState, progress,
	messageID int32, description string) {
	// 1. build response
	response := &proto.ActionResponse{
		ActionResponseState: &state,
		Progress:            &progress,
		ResponseDescription: &description,
	}
	// 2. built action result.
	result := &proto.ActionResult{
		Response: response,
	}

	handler.resultChan <- result
}

// Check if an action is a one-stage action.
// One stage action means after executor executes action, the action state is in Executed.
// If the state is Pending, then it means this action is a two stage action.
// Executor only finishes part of the required steps for a successful action.
func isOneStageAction(action *turboaction.TurboAction) (bool, error) {
	if action == nil {
		return false, errors.New("action passed in is nil.")
	}
	aType := action.Content.ActionType
	switch aType {
	case turboaction.ActionUnbind:
		return true, nil
	case turboaction.ActionProvision, turboaction.ActionMove:
		return false, nil
	}
	return false, fmt.Errorf("Unknown action type %s", aType)
}
