package action

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/openshift/cluster-api/pkg/client/clientset_generated/clientset"
	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	api "k8s.io/api/core/v1"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	kubeletclient "github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
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
	turboActionPodProvision     = turboActionType{proto.ActionItemDTO_PROVISION, proto.EntityDTO_CONTAINER_POD}
	turboActionPodSuspend       = turboActionType{proto.ActionItemDTO_SUSPEND, proto.EntityDTO_CONTAINER_POD}
	turboActionPodMove          = turboActionType{proto.ActionItemDTO_MOVE, proto.EntityDTO_CONTAINER_POD}
	turboActionContainerResize  = turboActionType{proto.ActionItemDTO_RIGHT_SIZE, proto.EntityDTO_CONTAINER}
	turboActionMachineProvision = turboActionType{proto.ActionItemDTO_PROVISION, proto.EntityDTO_VIRTUAL_MACHINE}
	turboActionMachineSuspend   = turboActionType{proto.ActionItemDTO_SUSPEND, proto.EntityDTO_VIRTUAL_MACHINE}
	turboActionControllerResize = turboActionType{proto.ActionItemDTO_RIGHT_SIZE, proto.EntityDTO_WORKLOAD_CONTROLLER}
)

type ActionHandlerConfig struct {
	clusterScraper *cluster.ClusterScraper
	cApiClient     *clientset.Clientset
	kubeletClient  *kubeletclient.KubeletClient
	StopEverything chan struct{}
	sccAllowedSet  map[string]struct{}
	cAPINamespace  string
	// ormClient provides the capability to update the corresponding CR for an Operator managed resource.
	ormClient *resourcemapping.ORMClient
}

func NewActionHandlerConfig(cApiNamespace string, cApiClient *clientset.Clientset, kubeletClient *kubeletclient.KubeletClient,
	clusterScraper *cluster.ClusterScraper, sccSupport []string, ormClient *resourcemapping.ORMClient) *ActionHandlerConfig {
	sccAllowedSet := make(map[string]struct{})
	for _, sccAllowed := range sccSupport {
		sccAllowedSet[strings.TrimSpace(sccAllowed)] = struct{}{}
	}
	glog.V(4).Infof("SCC's allowed: %s", sccAllowedSet)

	config := &ActionHandlerConfig{
		clusterScraper: clusterScraper,
		kubeletClient:  kubeletClient,
		StopEverything: make(chan struct{}),
		sccAllowedSet:  sccAllowedSet,
		cAPINamespace:  cApiNamespace,
		cApiClient:     cApiClient,
		ormClient:      ormClient,
	}

	return config
}

type ActionHandler struct {
	config *ActionHandlerConfig

	actionExecutors map[turboActionType]executor.TurboActionExecutor

	// concurrency control
	lockStore IActionLockStore

	podManager util.IPodManager
}

// Build new ActionHandler and start it.
func NewActionHandler(config *ActionHandlerConfig) *ActionHandler {
	lmap := util.NewExpirationMap(defaultActionCacheTTL)
	podsGetter := config.clusterScraper.Clientset.CoreV1()
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
	ae := executor.NewTurboK8sActionExecutor(c.clusterScraper, c.cApiClient, h.podManager, h.config.ormClient)

	reScheduler := executor.NewReScheduler(ae, c.sccAllowedSet)
	h.actionExecutors[turboActionPodMove] = reScheduler

	horizontalScaler := executor.NewHorizontalScaler(ae)
	h.actionExecutors[turboActionPodProvision] = horizontalScaler
	h.actionExecutors[turboActionPodSuspend] = horizontalScaler

	containerResizer := executor.NewContainerResizer(ae, c.kubeletClient, c.sccAllowedSet)
	h.actionExecutors[turboActionContainerResize] = containerResizer

	controllerResizer := executor.NewWorkloadControllerResizer(ae, c.kubeletClient, c.sccAllowedSet)
	h.actionExecutors[turboActionControllerResize] = controllerResizer

	// Only register the actions when API client is non-nil.
	if ok, err := executor.IsClusterAPIEnabled(c.cAPINamespace, c.cApiClient, c.clusterScraper.Clientset); ok && err == nil {
		machineScaler := executor.NewMachineActionExecutor(c.cAPINamespace, ae)
		h.actionExecutors[turboActionMachineProvision] = machineScaler
		h.actionExecutors[turboActionMachineSuspend] = machineScaler
	} else {
		glog.V(1).Info("the Cluster API is unavailable")
	}
}

// Implement ActionExecutorClient interface defined in Go SDK.
// Execute the current action and return the action result to SDK.
func (h *ActionHandler) ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
	accountValues []*proto.AccountValue,
	progressTracker sdkprobe.ActionProgressTracker) (*proto.ActionResult, error) {

	// 1. get the action, NOTE: only deal with one action item in current implementation.
	// Check if the action execution DTO is valid, including if the action is supported or not
	if err := h.checkActionExecutionDTO(actionExecutionDTO); err != nil {
		glog.Errorf("Invalid action %v: %v", actionExecutionDTO, err)
		return h.failedResult(err.Error()), err
	}

	// 2. keep sending fake progress to prevent timeout
	stop := make(chan struct{})
	defer close(stop)
	go keepAlive(progressTracker, stop)

	// 3. execute the action
	glog.V(3).Infof("Now wait for action result")
	err := h.execute(actionExecutionDTO.GetActionItem())
	if err != nil {
		glog.Errorf("action execution error %++v", err)
		return h.failedResult(err.Error()), nil
	}

	return h.goodResult(), nil
}

func isPodRelevantAction(actionItem *proto.ActionItemDTO) bool {
	entityType := actionItem.GetTargetSE().GetEntityType()
	return entityType == proto.EntityDTO_CONTAINER_POD ||
		entityType == proto.EntityDTO_CONTAINER
}

func (h *ActionHandler) execute(actionItems []*proto.ActionItemDTO) error {
	// Only acquire lock for pod actions so they can be sequentialized
	// We sequentialize pod actions because there could be different types of actions
	// generated for the same pod at the same time, e.g., resize and provision
	// For machine actions, we fail fast if there is already an action in progress for
	// the same machine, this is because there will not be resize action for machines,
	// and we do not want to queue multiple provision/suspend action for the same machine
	var pod *api.Pod
	// This works for all the actions
	// Actions with multiple action items (WORKLOAD_CONTROLLER) does not get the pod here; it
	// rather queries the pod again from the apiserver later in this flow.
	actionItem := actionItems[0]
	if isPodRelevantAction(actionItem) {
		// getLock() returns error if it times out (default timeout value is set in lockStore
		lock, err := h.lockStore.getLock(actionItem)
		if err != nil {
			return err
		}
		// Unlock the entity after the action execution is finished
		// defer is applied to the function scope
		defer glog.V(4).Infof("Action %s: releasing lock", actionItem.GetUuid())
		defer lock.ReleaseLock()
		lock.KeepRenewLock()
		// We need to get the k8s pod again as the previous action may have deleted the pod
		// and created a new one. In such case, the action should be applied to the new pod.
		pod, err = h.getRelatedPod(actionItem)
		if err != nil {
			return fmt.Errorf("cannot find the related pod for action item %s: %v",
				actionItem.GetUuid(), err)
		}
	}

	input := &executor.TurboActionExecutorInput{
		ActionItems: actionItems,
		Pod:         pod,
	}

	actionType := getTurboActionType(actionItem)
	worker := h.actionExecutors[actionType]
	output, err := worker.Execute(input)
	if err != nil {
		glog.Errorf("Failed to execute action %v on %v [%v]: %v",
			actionType.actionType, actionItem.GetTargetSE().GetEntityType(),
			actionItem.GetTargetSE().GetDisplayName(), err)
		return err
	}
	// Process the action execution output, including caching the pod name change.
	h.processOutput(output)
	return nil
}

// Finds the pod associated to the action item DTO. The pod, if any, will be used to lock the associated actions.
// - Pod Move/Provision/Suspend: uses the target SE in the action item
// - Container Resize: uses the hostedBy SE in the action item
func (h *ActionHandler) getRelatedPod(actionItem *proto.ActionItemDTO) (*api.Pod, error) {
	var podEntity *proto.EntityDTO
	actionType := getTurboActionType(actionItem)
	switch actionType {
	case turboActionContainerResize:
		podEntity = actionItem.GetHostedBySE()
	case turboActionPodMove, turboActionPodProvision, turboActionPodSuspend:
		podEntity = actionItem.GetTargetSE()
	case turboActionMachineProvision, turboActionMachineSuspend:
		// This branch is not called right now. Implement for the sake of completeness.
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported turbo action type %v", actionType)
	}

	if podEntity == nil {
		return nil, fmt.Errorf("nil pod entity in actionItem")
	}
	return h.podManager.GetPodFromDisplayNameOrUUID(podEntity.GetDisplayName(), podEntity.GetId())
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
	msg = "Action failed, " + msg

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

	// TODO: add timeout
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
		return fmt.Errorf("no action item found")
	}

	actionItem := actionItems[0]
	actionType := actionItem.GetActionType()
	targetSE := actionItem.GetTargetSE()
	if targetSE == nil {
		return fmt.Errorf("no target SE found")
	}

	glog.V(2).Infof("Received an action %v for entity %v [%v]",
		actionType, targetSE.GetEntityType(), targetSE.GetDisplayName())

	// Check if action is supported
	turboActionType := turboActionType{
		actionType:       actionType,
		targetEntityType: targetSE.GetEntityType(),
	}
	if _, supported := h.actionExecutors[turboActionType]; !supported {
		return fmt.Errorf("invalid action type %+v", turboActionType)
	}

	return nil
}
