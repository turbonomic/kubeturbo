package action

import (
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
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

	supportedActions := [...]turboActionType{turboActionPodProvision, turboActionPodMove, turboActionContainerResize,
		turboActionContainerPodSuspend}
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
	config.kubeClient = &client.Clientset{}
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

type mockProgressTrack struct{}

func (p *mockProgressTrack) UpdateProgress(actionState proto.ActionResponseState, description string, progress int32) {
}

type mockPodsGetter struct{}

func (p *mockPodsGetter) Pods(namespace string) v1.PodInterface {
	return &mockPodInterface{namespace}
}

type mockPodInterface struct {
	namespace string
}

func (p *mockPodInterface) Get(name string, options metav1.GetOptions) (*api.Pod, error) {
	pod := &api.Pod{}
	pod.Name = name
	pod.Namespace = p.namespace
	pod.UID = mockPodId
	pod.Status.Phase = api.PodRunning

	c := api.Container{Name: "container-foo", Image: "container-image-foo"}
	pod.Spec.Containers = []api.Container{c}

	return pod, nil
}

func (p *mockPodInterface) List(opts metav1.ListOptions) (*api.PodList, error) { return nil, nil }

func (p *mockPodInterface) Create(*api.Pod) (*api.Pod, error) { return nil, nil }

func (p *mockPodInterface) Update(*api.Pod) (*api.Pod, error) { return nil, nil }

func (p *mockPodInterface) UpdateStatus(*api.Pod) (*api.Pod, error) { return nil, nil }

func (p *mockPodInterface) Delete(name string, options *metav1.DeleteOptions) error { return nil }

func (p *mockPodInterface) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (p *mockPodInterface) Watch(opts metav1.ListOptions) (watch.Interface, error) { return nil, nil }

func (p *mockPodInterface) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *api.Pod, err error) {
	return nil, nil
}

func (p *mockPodInterface) Bind(binding *api.Binding) error { return nil }

func (p *mockPodInterface) Evict(eviction *policy.Eviction) error { return nil }

func (p *mockPodInterface) GetLogs(name string, opts *api.PodLogOptions) *restclient.Request {
	return nil
}
