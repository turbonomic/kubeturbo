package action

import (
	"fmt"
	"time"

	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/util"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	api "k8s.io/client-go/pkg/api/v1"
)

const (
	defaultActionCacheTTL  = time.Second * 100
	defaultPodNameCacheTTL = 10 * time.Minute
)

type turboActionType struct {
	actionType       proto.ActionItemDTO_ActionType
	targetEntityType proto.EntityDTO_EntityType
}

var (
	turboActionPodProvision        turboActionType = turboActionType{proto.ActionItemDTO_PROVISION, proto.EntityDTO_CONTAINER_POD}
	turboActionPodMove             turboActionType = turboActionType{proto.ActionItemDTO_MOVE, proto.EntityDTO_CONTAINER_POD}
	turboActionContainerResize     turboActionType = turboActionType{proto.ActionItemDTO_RIGHT_SIZE, proto.EntityDTO_CONTAINER}
	turboActionContainerPodSuspend turboActionType = turboActionType{proto.ActionItemDTO_SUSPEND, proto.EntityDTO_CONTAINER_POD}

	//turboActionUnbind          turboActionType = "unbind"
)

type ActionHandlerConfig struct {
	kubeClient     *client.Clientset
	kubeletClient  *kubeclient.KubeletClient
	StopEverything chan struct{}
}

func NewActionHandlerConfig(kubeClient *client.Clientset, kubeletClient *kubeclient.KubeletClient) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		kubeClient:     kubeClient,
		kubeletClient:  kubeletClient,
		StopEverything: make(chan struct{}),
	}

	return config
}

type ActionHandler struct {
	config *ActionHandlerConfig

	actionExecutors map[turboActionType]executor.TurboActionExecutor

	//concurrency control
	lockStore IActionLockStore

	podManager util.IPodManager
}

// Build new ActionHandler and start it.
func NewActionHandler(config *ActionHandlerConfig) *ActionHandler {
	lmap := util.NewExpirationMap(defaultActionCacheTTL)
	podsGetter := config.kubeClient.CoreV1()
	podCachedManager := util.NewPodCachedManager(turbostore.NewTurboCache(defaultPodNameCacheTTL).Cache, podsGetter)

	handler := &ActionHandler{
		config:          config,
		actionExecutors: make(map[turboActionType]executor.TurboActionExecutor),
		podManager:      podCachedManager,
	}

	go lmap.Run(config.StopEverything)
	handler.registerActionExecutors()
	handler.lockStore = newActionLockStore(lmap, handler.getRelatedPod)

	return handler
}

// Register supported action executor.
// As action executor is stateless, they can be safely reused.
func (h *ActionHandler) registerActionExecutors() {
	c := h.config
	ae := executor.NewTurboK8sActionExecutor(c.kubeClient, h.podManager)

	reScheduler := executor.NewReScheduler(ae)
	h.actionExecutors[turboActionPodMove] = reScheduler

	horizontalScaler := executor.NewHorizontalScaler(ae)
	h.actionExecutors[turboActionPodProvision] = horizontalScaler
	h.actionExecutors[turboActionContainerPodSuspend] = horizontalScaler

	containerResizer := executor.NewContainerResizer(ae, c.kubeletClient)
	h.actionExecutors[turboActionContainerResize] = containerResizer
}

// Implement ActionExecutorClient interface defined in Go SDK.
// Execute the current action and return the action result to SDK.
func (h *ActionHandler) ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
	accountValues []*proto.AccountValue,
	progressTracker sdkprobe.ActionProgressTracker) (*proto.ActionResult, error) {

	// 1. get the action, NOTE: only deal with one action item in current implementation.
	// Check if the action execution DTO is valid, including if the action is supported or not
	if err := h.checkActionExecutionDTO(actionExecutionDTO); err != nil {
		err := fmt.Errorf("Action is not valid: %v", err.Error())
		glog.Errorf(err.Error())
		return h.failedResult(err.Error()), err
	}

	actionItemDTO := actionExecutionDTO.GetActionItem()[0]

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

	// Acquire the lock for the actionItem. It blocks the action execution if the lock
	// is used by other action. It results in error return if timed out (set in lockStore).
	if lock, err := h.lockStore.getLock(actionItem); err != nil {
		return err
	} else {
		// Unlock the entity after the action execution is finished
		defer glog.V(4).Infof("Action %s: releasing lock", actionItem.GetUuid())
		defer lock.ReleaseLock()
		lock.KeepRenewLock()
	}

	// After getting the lock, need to get the k8s pod again as the previous action could delete the pod and create a new one.
	// In such case, the action should be applied on the new pod.
	// Currently, all actions need to get its related pod. If not needed, the pod is nil.
	pod := h.getRelatedPod(actionItem)

	if pod == nil {
		err := fmt.Errorf("Cannot find the related pod for action item %s", actionItem.GetUuid())
		return err
	}

	input := &executor.TurboActionExecutorInput{
		ActionItem: actionItem,
		Pod:        pod,
	}
	actionType := getTurboActionType(actionItem)
	worker := h.actionExecutors[actionType]
	output, err := worker.Execute(input)

	if err != nil {
		msg := fmt.Errorf("Action %v on %s failed.", actionType, actionItem.GetTargetSE().GetEntityType())
		glog.Errorf(msg.Error())
		return err
	}

	// Process the action execution output, including caching the pod name change.
	h.processOutput(output)

	return nil
}

// Finds the pod associated to the action item dto. The pod, if any, will be used to lock the associated actions.
// Currently, we consider three action types:
// - Pod Move/Provision: returns the pod, which is the target SE in the action item
// - Container Resize: returns the pod, which is the hostedBy SE in the action item
func (h *ActionHandler) getRelatedPod(actionItem *proto.ActionItemDTO) *api.Pod {
	actionType := getTurboActionType(actionItem)
	var podEntity *proto.EntityDTO
	se := actionItem.GetTargetSE()

	switch actionType {
	case turboActionContainerResize:
		podEntity = actionItem.GetHostedBySE()
	case turboActionPodMove:
		podEntity = se
	case turboActionPodProvision:
		podEntity = se
	case turboActionContainerPodSuspend:
		podEntity = se
	default:
		return nil
	}

	pod, err := h.podManager.GetPodFromDisplayNameOrUUID(podEntity.GetDisplayName(), podEntity.GetId())
	if err != nil {
		glog.Errorf("failed to get Pod %s with id %s: %v", podEntity.GetDisplayName(), podEntity.GetId(), err)
		return nil
	}

	return pod
}

// Processes the output of the action execution generated by the executor.
// The pod change made by the executor, if any, will be cached in the pod manager for
// further actions on the same pod.
func (h *ActionHandler) processOutput(output *executor.TurboActionExecutorOutput) {
	if output == nil || !output.Succeeded || output.OldPod == nil || output.NewPod == nil {
		return
	}

	// If the action succeeded, cache the pod name change for the following actions.
	h.podManager.CachePod(output.OldPod, output.NewPod)
}

// Get the associated turbo action type of the action item dto
func getTurboActionType(ai *proto.ActionItemDTO) turboActionType {
	return turboActionType{ai.GetActionType(), ai.GetTargetSE().GetEntityType()}
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
				glog.V(3).Infof("action keepAlive goroutine exit.")
				return
			case <-t.C:
			}
		}
	}()
}

// Checks if the action execution DTO includes action item and the target SE. Also, check if
// the action type is supported by kubeturbo.
func (h *ActionHandler) checkActionExecutionDTO(actionExecutionDTO *proto.ActionExecutionDTO) error {
	actionItems := actionExecutionDTO.GetActionItem()

	if actionItems == nil || len(actionItems) == 0 || actionItems[0] == nil {
		return fmt.Errorf("Action execution (%v) validation failed: no action item found", actionExecutionDTO)

	}

	ai := actionItems[0]

	if ai.GetTargetSE() == nil {
		return fmt.Errorf("Action execution (%v) validation failed: no target SE found", actionExecutionDTO)
	}

	actionType := turboActionType{ai.GetActionType(), ai.GetTargetSE().GetEntityType()}
	glog.V(2).Infof("Receive a action request of type: %++v", actionType)

	// Check if action is supported
	if _, supported := h.actionExecutors[actionType]; !supported {
		return fmt.Errorf("Action execution (%v) validation failed: not supported type %++v", actionExecutionDTO, actionType)
	}

	return nil
}
