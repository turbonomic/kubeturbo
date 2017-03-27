package action

import (
	"errors"
	"fmt"

	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/supervisor"
	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	turboscheduler "github.com/turbonomic/kubeturbo/pkg/scheduler"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ActionHandlerConfig struct {
	kubeClient     *client.Client
	broker         turbostore.Broker
	StopEverything chan struct{}
}

func NewActionHandlerConfig(kubeClient *client.Client, broker turbostore.Broker) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		kubeClient: kubeClient,
		broker:     broker,

		StopEverything: make(chan struct{}),
	}

	return config
}

type ActionHandler struct {
	config *ActionHandlerConfig

	actionExecutors map[turboaction.TurboActionType]executor.TurboActionExecutor

	actionSupervisor *supervisor.ActionSupervisor

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
	executedActionChan := make(chan *turboaction.TurboAction)
	succeededActionChan := make(chan *turboaction.TurboAction)
	failedActionChan := make(chan *turboaction.TurboAction)

	supervisorConfig := supervisor.NewActionSupervisorConfig(config.kubeClient, executedActionChan, succeededActionChan, failedActionChan)
	actionSupervisor := supervisor.NewActionSupervisor(supervisorConfig)

	handler := &ActionHandler{
		config:           config,
		actionExecutors:  make(map[turboaction.TurboActionType]executor.TurboActionExecutor),
		actionSupervisor: actionSupervisor,

		scheduler: scheduler,

		executedActionChan:  executedActionChan,
		succeededActionChan: succeededActionChan,
		failedActionChan:    failedActionChan,

		resultChan: make(chan *proto.ActionResult),
	}

	handler.registerActionExecutors()
	handler.Start()
	return handler
}

// Register supported action executor.
// As action executor is stateless, they can be safely reused.
func (h *ActionHandler) registerActionExecutors() {
	reScheduler := executor.NewReScheduler(h.config.kubeClient, h.config.broker)
	h.actionExecutors[turboaction.ActionMove] = reScheduler

	horizontalScaler := executor.NewHorizontalScaler(h.config.kubeClient, h.config.broker, h.scheduler)
	h.actionExecutors[turboaction.ActionProvision] = horizontalScaler
	h.actionExecutors[turboaction.ActionUnbind] = horizontalScaler
}

// Start watching succeeded and failed turbo actions.
// Also start ActionSupervisor to determine the final status of executed VMTEvents.
func (h *ActionHandler) Start() {
	go wait.Until(h.getNextSucceededTurboAction, 0, h.config.StopEverything)
	go wait.Until(h.getNextFailedTurboAction, 0, h.config.StopEverything)

	h.actionSupervisor.Start()
}

func (h *ActionHandler) getNextSucceededTurboAction() {
	event := <-h.succeededActionChan
	glog.V(3).Infof("Succeeded event is %v", event)
	content := event.Content

	glog.V(2).Infof("Action %s for %s-%s succeeded.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
	progress := int32(100)
	h.sendActionResult(proto.ActionResponseState_SUCCEEDED, progress, "Success")
}

func (h *ActionHandler) getNextFailedTurboAction() {
	event := <-h.failedActionChan

	glog.V(3).Infof("Failed event is %v", event)
	content := event.Content

	glog.V(2).Infof("Action %s for %s-%s failed.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
	progress := int32(0)
	h.sendActionResult(proto.ActionResponseState_FAILED, progress, "Failed 1")
}

// Implement ActionExecutorClient interface defined in Go SDK.
// Execute the current action and return the action result.
func (h *ActionHandler) ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
	accountValues []*proto.AccountValue,
	progressTracker sdkprobe.ActionProgressTracker) (*proto.ActionResult, error) {

	actionItems := actionExecutionDTO.GetActionItem()
	// TODO: only deal with one action item.
	actionItemDTO := actionItems[0]
	go h.execute(actionItemDTO)

	glog.V(3).Infof("Now wait for action result")
	result := <-h.resultChan
	glog.V(4).Infof("Action result is %++v", result)
	// TODO: currently the code in SDK make it share the actionExecution client between different workers. Once it is changed, need to close the channel.
	//close(h.config.StopEverything)
	return result, nil
}

func (h *ActionHandler) execute(actionItem *proto.ActionItemDTO) {
	actionType, err := getActionTypeFromActionItemDTO(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute action: %s", err)
		errorMsg := fmt.Sprintf("Failed to execute action: %s", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), errorMsg)
		return
	}
	executor, exist := h.actionExecutors[actionType]
	if !exist {
		glog.Errorf("action type %s is not support", actionType)
		errorMsg := fmt.Sprintf("Failed to execute action. The action %s is currently not supported", actionType)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), errorMsg)
		return
	}

	action, err := executor.Execute(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute action: %s", err)
		errorMsg := fmt.Sprintf("Failed to execute action: %s", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), errorMsg)
		return
	}
	h.executedActionChan <- action
}

func getActionTypeFromActionItemDTO(actionItem *proto.ActionItemDTO) (turboaction.TurboActionType, error) {
	var actionType turboaction.TurboActionType

	if actionItem == nil {
		return actionType, errors.New("ActionItem received in is null")
	}
	glog.V(3).Infof("Receive a %s action request.", actionItem.GetActionType())

	switch actionItem.GetActionType() {
	case proto.ActionItemDTO_MOVE:
		// Here we must make sure the TargetSE is a Pod and NewSE is either a VirtualMachine or a PhysicalMachine.
		if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_CONTAINER_POD {
			// A regular MOVE action
			actionType = turboaction.ActionMove
		} else if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_VIRTUAL_APPLICATION {
			// An UnBind action
			actionType = turboaction.ActionUnbind
		} else {
			// NOT Supported
			return actionType, fmt.Errorf("The service entity to be moved is not a "+
				"Pod. Got %s", actionItem.GetTargetSE().GetEntityType())
		}
		break
	case proto.ActionItemDTO_PROVISION:
		// A Provision action
		actionType = turboaction.ActionProvision
		break
	default:
		return actionType, fmt.Errorf("Action %s not supported", actionItem.GetActionType())
	}

	return actionType, nil
}

// Send action response to Turbonomic server.
func (handler *ActionHandler) sendActionResult(state proto.ActionResponseState, progress int32, description string) {
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
