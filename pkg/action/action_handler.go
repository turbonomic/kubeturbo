package action

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/action/executor"
	"github.com/vmturbo/kubeturbo/pkg/action/supervisor"
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/vmturbo/kubeturbo/pkg/turbostore"
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
	config           *ActionHandlerConfig
	actionExecutor   *executor.ActionExecutor
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

	actionExecutor := executor.NewVMTActionExecutor(config.kubeClient, config.broker, scheduler, executedActionChan)

	handler := &ActionHandler{
		config:           config,
		actionExecutor:   actionExecutor,
		actionSupervisor: actionSupervisor,

		scheduler: scheduler,

		executedActionChan:  executedActionChan,
		succeededActionChan: succeededActionChan,
		failedActionChan:    failedActionChan,

		resultChan: make(chan *proto.ActionResult),
	}

	handler.Start()
	return handler
}

// Start watching successful and failed turbo actions.
// Also start ActionSupervisor to determine the final status of executed VMTEvents.
func (h *ActionHandler) Start() {
	go wait.Until(h.getNextSucceededVMTEvent, 0, h.config.StopEverything)
	go wait.Until(h.getNextFailedVMTEvent, 0, h.config.StopEverything)

	h.actionSupervisor.Start()
	glog.Infof("Finised Start handler")
}

func (h *ActionHandler) getNextSucceededVMTEvent() {
	event := <-h.succeededActionChan
	glog.V(3).Infof("Succeeded event is %v", event)
	content := event.Content

	glog.V(2).Infof("Action %s for %s-%s succeeded.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
	progress := int32(100)
	h.sendActionResult(proto.ActionResponseState_SUCCEEDED, progress, "Success")
}

func (h *ActionHandler) getNextFailedVMTEvent() {
	event := <-h.failedActionChan

	glog.V(3).Infof("Failed event is %v", event)
	content := event.Content

	glog.V(2).Infof("Action %s for %s-%s failed.", content.ActionType, content.TargetObject.TargetObjectType, content.TargetObject.TargetObjectName)
	progress := int32(0)
	h.sendActionResult(proto.ActionResponseState_FAILED, progress, "Failed 1")
}

// Execute the current action and return the action result.
func (h *ActionHandler) ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
	accountValues []*proto.AccountValue,
	progressTracker sdkprobe.ActionProgressTracker) (*proto.ActionResult, error) {

	actionItems := actionExecutionDTO.GetActionItem()
	// TODO only deal with one action item.
	actionItemDTO := actionItems[0]
	h.execute(actionItemDTO)

	glog.Infof("Waiting for result")
	result := <-h.resultChan
	glog.Infof("Result is %++v", result)
	//close(h.config.StopEverything)
	return result, nil
}

func (h *ActionHandler) execute(actionItem *proto.ActionItemDTO) {
	//	actionEvent, err := h.actionExecutor.ExecuteAction(actionItem)
	_, err := h.actionExecutor.ExecuteAction(actionItem)

	if err != nil {
		glog.Errorf("Error execute action: %s", err)
		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), "Failed to execute action")
		return
	}
	//	isOne, err := isOneStageAction(actionEvent)
	//	if err != nil {
	//		glog.Errorf("Error execute action: %s", err)
	//		h.sendActionResult(proto.ActionResponseState_FAILED, int32(0), "Failed to check action")
	//		return
	//	}
	//	if isOne {
	//		// The action is in Executed stage, then send the action to executedChan
	//		glog.V(4).Infof("Finished executing action %+v, actionEvent")
	//		h.executedActionChan <- actionEvent
	//		return
	//	}
	//
	//	// TODO we must make sure there is no NPE
	//	if actionEvent.Content.TargetObject.TargetObjectType == turboaction.TypePod {
	//		glog.V(3).Infof("A pod related aciton is in 2nd stage:%++v", actionEvent)
	//		key := actionEvent.Content.ParentObjectRef.ParentObjectUID
	//		if key == "" {
	//			key = actionEvent.Content.TargetObject.TargetObjectUID
	//		}
	//		glog.V(3).Infof("the listening key is %s", key)
	//		podConsumer := turbostore.NewPodConsumer(*actionItem.Uuid, key, h.config.broker)
	//		p, ok := <-podConsumer.WaitPod()
	//		if !ok {
	//			return
	//		}
	//		h.HandlePodAction(p, actionEvent)
	//		glog.Infof("HandlePod action  ends")
	//		podConsumer.Leave(key, h.config.broker)
	//		glog.Infof("Pod consumer has unsubscribed.")
	//	}
	//	glog.Infof("########### finish execute() #########")
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
	glog.Infof("Send result to result chan %++v", result)
	handler.resultChan <- result
	glog.Infof("Result has been sent and received")
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
