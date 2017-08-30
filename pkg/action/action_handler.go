package action

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/supervisor"
	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	turboscheduler "github.com/turbonomic/kubeturbo/pkg/scheduler"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	defaultActionCacheTTL = time.Second * 100
)

type ActionHandlerConfig struct {
	kubeClient     *client.Clientset
	broker         turbostore.Broker
	StopEverything chan struct{}

	//for moveAction
	k8sVersion        string
	noneSchedulerName string
}

func NewActionHandlerConfig(kubeClient *client.Clientset, broker turbostore.Broker, k8sVersion, noneSchedulerName string) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		kubeClient: kubeClient,
		broker:     broker,

		k8sVersion:        k8sVersion,
		noneSchedulerName: noneSchedulerName,

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

	//concurrency control
	lockMap *util.ExpirationMap
}

// Build new ActionHandler and start it.
func NewActionHandler(config *ActionHandlerConfig, scheduler *turboscheduler.TurboScheduler) *ActionHandler {
	executedActionChan := make(chan *turboaction.TurboAction)
	succeededActionChan := make(chan *turboaction.TurboAction)
	failedActionChan := make(chan *turboaction.TurboAction)

	supervisorConfig := supervisor.NewActionSupervisorConfig(config.kubeClient, executedActionChan, succeededActionChan, failedActionChan)
	actionSupervisor := supervisor.NewActionSupervisor(supervisorConfig)

	lmap := util.NewExpirationMap(defaultActionCacheTTL)

	handler := &ActionHandler{
		config:           config,
		actionExecutors:  make(map[turboaction.TurboActionType]executor.TurboActionExecutor),
		actionSupervisor: actionSupervisor,

		scheduler: scheduler,
		lockMap:   lmap,

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
	reScheduler := executor.NewReScheduler(h.config.kubeClient, h.config.broker, h.config.k8sVersion, h.config.noneSchedulerName, h.lockMap)
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

	go h.lockMap.Run(h.config.StopEverything)
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
	msg := fmt.Sprintf("Action %s on %s failed.", content.ActionType, content.TargetObject.TargetObjectType)
	h.sendActionResult(proto.ActionResponseState_FAILED, progress, msg)
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

	stop := make(chan struct{})
	defer close(stop)
	keepAlive(progressTracker, stop)

	glog.V(3).Infof("Now wait for action result")
	result := <-h.resultChan
	glog.V(4).Infof("Action result is %++v", result)
	// TODO: currently the code in SDK make it share the actionExecution client between different workers. Once it is changed, need to close the channel.
	//close(h.config.StopEverything)
	return result, nil
}

func keepAlive(tracker sdkprobe.ActionProgressTracker, stop chan struct{}) {

	//TODO: add timeout
	go func() {
		var progress int32 = 0
		state := proto.ActionResponseState_IN_PROGRESS

		for {
			progress = progress + 1
			if progress > 99 {
				progress = 99
			}

			tracker.UpdateProgress(state, "in progress", progress)

			t := time.NewTimer(time.Second * 3)
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
		glog.V(3).Infof("action keepAlive goroutine exit.")
	}()
}

func (h *ActionHandler) execute(actionItem *proto.ActionItemDTO) {
	actionType, err := getActionTypeFromActionItemDTO(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute action: %v", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), err.Error())
		return
	}
	executor, exist := h.actionExecutors[actionType]
	if !exist {
		glog.Errorf("action type %s is not support", actionType)
		msg := fmt.Sprintf("Action %s on %s is not supported.", actionType, actionItem.GetTargetSE().GetEntityType())
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), msg)
		return
	}

	action, err := executor.Execute(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute action: %s", err)
		msg := fmt.Sprintf("Action %s on %s failed.", actionType, actionItem.GetTargetSE().GetEntityType())
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), msg)
		return
	}
	if action.Status == turboaction.Success {
		h.sendActionResult(proto.ActionResponseState_SUCCEEDED, int32(100), "Success")
		return
	}

	//send to channel to check the final status of the action if action.Status == turboaction.Executed
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
		return actionType, fmt.Errorf("Action %s is not supported", actionItem.GetActionType())
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
