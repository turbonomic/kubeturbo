package action

import (
	"fmt"
	"time"

	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type turboActionType string

const (
	defaultActionCacheTTL                      = time.Second * 100
	turboActionProvision       turboActionType = "provision"
	turboActionMove            turboActionType = "move"
	turboActionUnbind          turboActionType = "unbind"
	turboActionContainerResize turboActionType = "resizeContainer"
)

type ActionHandlerConfig struct {
	kubeClient     *client.Clientset
	kubeletClient  *kubelet.KubeletClient
	StopEverything chan struct{}

	//for moveAction
	k8sVersion        string
	noneSchedulerName string
	stitchType        stitching.StitchingPropertyType
}

func NewActionHandlerConfig(kubeClient *client.Clientset, kubeletClient *kubelet.KubeletClient, k8sVersion, noneSchedulerName string, stype stitching.StitchingPropertyType) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		kubeClient:    kubeClient,
		kubeletClient: kubeletClient,

		k8sVersion:        k8sVersion,
		noneSchedulerName: noneSchedulerName,
		stitchType:        stype,

		StopEverything: make(chan struct{}),
	}

	return config
}

type ActionHandler struct {
	config *ActionHandlerConfig

	actionExecutors map[turboActionType]executor.TurboActionExecutor

	//concurrency control
	lockMap *util.ExpirationMap
}

// Build new ActionHandler and start it.
func NewActionHandler(config *ActionHandlerConfig) *ActionHandler {
	lmap := util.NewExpirationMap(defaultActionCacheTTL)
	handler := &ActionHandler{
		config:          config,
		actionExecutors: make(map[turboActionType]executor.TurboActionExecutor),
		lockMap:         lmap,
	}

	go lmap.Run(config.StopEverything)
	handler.registerActionExecutors()
	return handler
}

// Register supported action executor.
// As action executor is stateless, they can be safely reused.
func (h *ActionHandler) registerActionExecutors() {
	c := h.config
	reScheduler := executor.NewReScheduler(c.kubeClient, c.k8sVersion, c.noneSchedulerName, h.lockMap, c.stitchType)
	h.actionExecutors[turboActionMove] = reScheduler

	horizontalScaler := executor.NewHorizontalScaler(c.kubeClient, h.lockMap)
	h.actionExecutors[turboActionProvision] = horizontalScaler
	h.actionExecutors[turboActionUnbind] = horizontalScaler

	containerResizer := executor.NewContainerResizer(c.kubeClient, c.kubeletClient, c.k8sVersion, c.noneSchedulerName, h.lockMap)
	h.actionExecutors[turboActionContainerResize] = containerResizer
}

// Implement ActionExecutorClient interface defined in Go SDK.
// Execute the current action and return the action result to SDK.
func (h *ActionHandler) ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
	accountValues []*proto.AccountValue,
	progressTracker sdkprobe.ActionProgressTracker) (*proto.ActionResult, error) {

	// 1. get the action, NOTE: only deal with one action item in current implementation.
	actionItems := actionExecutionDTO.GetActionItem()
	actionItemDTO := actionItems[0]

	// 2. keep sending fake progress to prevent timeout
	stop := make(chan struct{})
	defer close(stop)
	go keepAlive(progressTracker, stop)

	// 3. execute the action
	glog.V(3).Infof("Now wait for action result")
	err := h.execute(actionItemDTO)
	if err != nil {
		return h.failedResult(err.Error()), nil
	}

	return h.goodResult(), nil
}

func (h *ActionHandler) execute(actionItem *proto.ActionItemDTO) error {
	actionType, err := getActionTypeFromActionItemDTO(actionItem)
	if err != nil {
		glog.Errorf("Failed to execute action: %v", err)
		return err
	}

	worker, exist := h.actionExecutors[actionType]
	if !exist {
		msg := fmt.Errorf("Action %s on %s is not supported.", actionType, actionItem.GetTargetSE().GetEntityType())
		glog.Errorf(msg.Error())
		return msg
	}

	err = worker.Execute(actionItem)
	if err != nil {
		msg := fmt.Errorf("Action %s on %s failed.", actionType, actionItem.GetTargetSE().GetEntityType())
		glog.Errorf(msg.Error())
		return err
	}

	return nil
}

func getActionTypeFromActionItemDTO(actionItem *proto.ActionItemDTO) (turboActionType, error) {
	var actionType turboActionType

	if actionItem == nil {
		return actionType, fmt.Errorf("ActionItem received in is null")
	}
	glog.V(3).Infof("Receive a %s action request.", actionItem.GetActionType())
	objectType := actionItem.GetTargetSE().GetEntityType()

	switch actionItem.GetActionType() {
	case proto.ActionItemDTO_MOVE:
		// Here we must make sure the TargetSE is a Pod and NewSE is either a VirtualMachine or a PhysicalMachine.
		if objectType == proto.EntityDTO_CONTAINER_POD {
			// A regular MOVE action
			return turboActionMove, nil
		} else if objectType == proto.EntityDTO_VIRTUAL_APPLICATION {
			// An UnBind action
			return turboActionUnbind, nil
		}
	case proto.ActionItemDTO_PROVISION:
		// A Provision action
		return turboActionProvision, nil
	case proto.ActionItemDTO_RIGHT_SIZE:
		if objectType == proto.EntityDTO_CONTAINER {
			return turboActionContainerResize, nil
		}
	}

	err := fmt.Errorf("Unsupported action[%v] for objectType[%v]", actionItem.GetActionType(), objectType)
	glog.Error(err)
	return actionType, err
}

func (h *ActionHandler) goodResult() *proto.ActionResult {

	state := proto.ActionResponseState_SUCCEEDED
	progress := int32(100)
	msg := "Success"

	res := &proto.ActionResponse{
		ActionResponseState: &state,
		Progress:            &progress,
		ResponseDescription: &msg,
	}

	return &proto.ActionResult{
		Response: res,
	}
}

func (h *ActionHandler) failedResult(msg string) *proto.ActionResult {

	state := proto.ActionResponseState_FAILED
	progress := int32(0)
	msg = "Failed"

	res := &proto.ActionResponse{
		ActionResponseState: &state,
		Progress:            &progress,
		ResponseDescription: &msg,
	}

	return &proto.ActionResult{
		Response: res,
	}
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
