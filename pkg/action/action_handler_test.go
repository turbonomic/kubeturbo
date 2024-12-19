package action

import (
	"context"
	"testing"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	discoveryutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.ibm.com/turbonomic/kubeturbo/pkg/turbostore"
)

const (
	mockPodNamespace = "workspace-foo"
	mockPodName      = "pod-foo"
	mockPodDispName  = mockPodNamespace + "/" + mockPodName
	mockPodId        = "pod-foo-id"
)

func TestActionHandler_registerActionExecutors(t *testing.T) {
	h := NewActionHandler(newActionHandlerConfig())

	h.registerActionExecutors()

	supportedActions := [...]turboActionType{
		turboActionPodProvision, turboActionControllerScale, turboActionPodMove,
		turboActionContainerResize, turboActionPodSuspend, turboActionControllerResize,
		turboActionMachineProvision, turboActionMachineSuspend,
	}
	m := h.actionExecutors
	if len(m) != len(supportedActions) {
		t.Errorf("Action handler supports %d action types but got %d", len(supportedActions), len(m))
	}

	for _, action := range supportedActions {
		if _, ok := m[action]; !ok {
			t.Errorf("Missing action executor for %v", action)
		}
	}
}

func TestActionHandler_ExecuteAction_Succeed(t *testing.T) {
	var podCache turbostore.ITurboCache = turbostore.NewTurboCache(defaultPodNameCacheTTL).Cache
	h := newActionHandler(podCache)
	targetSE := newTargetSE()
	actionExecutionDTO := newActionExecutionDTO(proto.ActionItemDTO_MOVE, targetSE)
	mockProgressTrack := &mockProgressTrack{}
	result, err := h.ExecuteAction(actionExecutionDTO, nil, mockProgressTrack)
	if err != nil {
		t.Errorf("ActionHandler.ExecuteAction(): error = %v", err)
	}

	// Check action response state
	if *result.Response.ActionResponseState != proto.ActionResponseState_SUCCEEDED {
		t.Errorf("ActionHandler.ExecuteAction(): action response (%v) is not %v",
			result.Response.ActionResponseState, proto.ActionResponseState_SUCCEEDED)
	}

	// Check if the pod change cached correctly
	if cachedId, ok := podCache.Get(*targetSE.Id); !ok {
		t.Errorf("The pod change is not cached")
	} else if cachedId != (*targetSE.Id + "-c") {
		t.Errorf("The cached pod %s is not correct: %s", cachedId, *targetSE.Id)
	}
}

func TestActionHandler_ExecuteAction_Unsupported_Action(t *testing.T) {
	var podCache turbostore.ITurboCache = turbostore.NewTurboCache(defaultPodNameCacheTTL).Cache
	h := newActionHandler(podCache)
	targetSE := newTargetSE()
	actionExecutionDTO := newActionExecutionDTO(proto.ActionItemDTO_RESIZE, targetSE)
	mockProgressTrack := &mockProgressTrack{}
	result, err := h.ExecuteAction(actionExecutionDTO, nil, mockProgressTrack)

	if err == nil {
		t.Errorf("Expect error of action not supported")
	}

	// Check action response state
	if *result.Response.ActionResponseState != proto.ActionResponseState_FAILED {
		t.Errorf("ActionHandler.ExecuteAction(): action response (%v) is not %v",
			result.Response.ActionResponseState, proto.ActionResponseState_FAILED)
	}
}

func newActionHandler(cache turbostore.ITurboCache) *ActionHandler {
	config := newActionHandlerConfig()
	actionExecutors := make(map[turboActionType]executor.TurboActionExecutor)
	actionExecutors[turboActionPodMove] = &mockExecutor{}

	mockPodsGetter := &mockPodsGetter{}

	handler := &ActionHandler{}
	handler.config = config
	handler.actionExecutors = actionExecutors
	handler.podManager = util.NewPodCachedManager(cache, mockPodsGetter)
	lmap := util.NewExpirationMap(defaultActionCacheTTL)
	handler.lockStore = newActionLockStore(lmap, handler.getRelatedPod)

	go lmap.Run(config.StopEverything)
	return handler
}

func newActionHandlerConfig() *ActionHandlerConfig {
	config := &ActionHandlerConfig{}

	config.StopEverything = make(chan struct{})
	config.clusterScraper = cluster.NewClusterScraper(nil, &client.Clientset{}, nil, nil,
		nil, nil, "")
	config.kubeletClient = &kubeclient.KubeletClient{}

	return config
}

func newActionExecutionDTO(actionType proto.ActionItemDTO_ActionType, targetSE *proto.EntityDTO) *proto.ActionExecutionDTO {
	ai := &proto.ActionItemDTO{}
	ai.TargetSE = targetSE
	ai.ActionType = &actionType
	dto := &proto.ActionExecutionDTO{}
	dto.ActionItem = []*proto.ActionItemDTO{ai}

	return dto
}

func newTargetSE() *proto.EntityDTO {
	entityType := proto.EntityDTO_CONTAINER_POD
	podDispName := mockPodDispName
	podId := mockPodId
	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &podDispName
	se.Id = &podId

	return se
}

type mockExecutor struct{}

func (m *mockExecutor) Execute(input *executor.TurboActionExecutorInput) (*executor.TurboActionExecutorOutput, error) {
	oldPod := input.Pod
	pod := &api.Pod{}
	pod.Name = oldPod.Name + "-c"
	pod.UID = oldPod.UID + "-c"

	output := &executor.TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    oldPod,
		NewPod:    pod,
	}
	return output, nil
}

func (m *mockExecutor) ExecuteList(input *executor.TurboActionExecutorInput) (*executor.TurboActionExecutorOutput, error) {
	output := &executor.TurboActionExecutorOutput{
		Succeeded: true,
	}
	return output, nil
}

type mockProgressTrack struct{}

func (p *mockProgressTrack) UpdateProgress(actionState proto.ActionResponseState, description string, progress int32) {
}

type mockPodsGetter struct{}

func (p *mockPodsGetter) Pods(namespace string) v1.PodInterface {
	return &mockPodInterface{
		discoveryutil.MockPodInterface{
			Namespace: namespace,
		},
	}
}

type mockPodInterface struct {
	discoveryutil.MockPodInterface
}

func (p *mockPodInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*api.Pod, error) {
	pod := &api.Pod{}
	pod.Name = name
	pod.Namespace = p.Namespace
	pod.UID = mockPodId
	pod.Status.Phase = api.PodRunning

	c := api.Container{Name: "container-foo", Image: "container-image-foo"}
	pod.Spec.Containers = []api.Container{c}

	return pod, nil
}
