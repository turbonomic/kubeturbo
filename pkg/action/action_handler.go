package action

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/action/supervisor"
	vmtcache "github.com/vmturbo/kubeturbo/pkg/cache"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	comm "github.com/vmturbo/vmturbo-go-sdk/pkg/communication"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ActionHandlerConfig struct {
	etcdStorage            storage.Storage
	kubeClient             *client.Client
	SucceededVMTEventQueue *vmtcache.HashedFIFO
	FailedVMTEventQueue    *vmtcache.HashedFIFO
	StopEverything         chan struct{}
}

func NewActionHandlerConfig(kubeClient *client.Client, etcdStorage storage.Storage,
	wsComm *comm.WebSocketCommunicator) *ActionHandlerConfig {
	config := &ActionHandlerConfig{
		etcdStorage: etcdStorage,
		kubeClient:  kubeClient,

		SucceededVMTEventQueue: vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		FailedVMTEventQueue:    vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),

		StopEverything: make(chan struct{}),
	}

	vmtcache.NewReflector(config.createSucceededVMTEventsLW(), &registry.VMTEvent{},
		config.SucceededVMTEventQueue, 0).RunUntil(config.StopEverything)
	vmtcache.NewReflector(config.createFailedVMTEventsLW(), &registry.VMTEvent{},
		config.FailedVMTEventQueue, 0).RunUntil(config.StopEverything)
	return config

}

func (config *ActionHandlerConfig) createSucceededVMTEventsLW() *vmtcache.ListWatch {
	return vmtcache.NewListWatchFromStorage(config.etcdStorage, "vmtevents", api.NamespaceAll,
		func(obj interface{}) bool {
			vmtEvent, ok := obj.(*registry.VMTEvent)
			if ok && vmtEvent.Status == registry.Success {
				return true
			}
			return false
		})
}

func (config *ActionHandlerConfig) createFailedVMTEventsLW() *vmtcache.ListWatch {
	return vmtcache.NewListWatchFromStorage(config.etcdStorage, "vmtevents", api.NamespaceAll,
		func(obj interface{}) bool {
			vmtEvent, ok := obj.(*registry.VMTEvent)
			if ok && vmtEvent.Status == registry.Fail {
				return true
			}
			return false
		})
}

type ActionHandler struct {
	config           *ActionHandlerConfig
	actionExecutor   *VMTActionExecutor
	actionSupervisor *supervisor.VMTActionSupervisor

	resultChan chan *proto.ActionResult
}

func NewActionHandler(config *ActionHandlerConfig) *ActionHandler {
	supervisorConfig := supervisor.NewActionSupervisorConfig(config.kubeClient, config.etcdStorage)
	supervisor := supervisor.NewActionSupervisor(supervisorConfig)

	executor := NewVMTActionExecutor(config.kubeClient, config.etcdStorage)

	return &ActionHandler{
		config:           config,
		actionExecutor:   executor,
		actionSupervisor: supervisor,

		resultChan: make(chan *proto.ActionResult),
	}
}

// Start watching successful and failed VMTEvents.
// Also start ActionSupervior to determine the final status of executed VMTEvents.
func (this *ActionHandler) Start() {
	go wait.Until(this.getNextSucceededVMTEvent, 0, this.config.StopEverything)
	go wait.Until(this.getNextFailedVMTEvent, 0, this.config.StopEverything)

	this.actionSupervisor.Start()
}

func (this *ActionHandler) getNextSucceededVMTEvent() {
	e, err := this.config.SucceededVMTEventQueue.Pop(nil)
	if err != nil {
		//TODO
	}
	event := e.(*registry.VMTEvent)
	glog.V(3).Infof("Succeeded event is %v", event)
	content := event.Content
	msgID := int32(content.VMTMessageID)
	if msgID > -1 {
		glog.V(2).Infof("Action %s for %s succeeded.", content.ActionType, content.TargetSE)
		progress := int32(100)
		this.sendActionResult(proto.ActionResponseState_SUCCEEDED, progress, msgID, "Success")
	}
	// TODO init discovery
}

func (this *ActionHandler) getNextFailedVMTEvent() {
	e, err := this.config.SucceededVMTEventQueue.Pop(nil)
	if err != nil {
		//TODO
	}
	event := e.(*registry.VMTEvent)

	glog.V(3).Infof("Failed event is %v", event)
	// TODO, send back action failed
	content := event.Content
	msgID := int32(content.VMTMessageID)
	if msgID > -1 {
		glog.V(2).Infof("Action %s for %s failed.", content.ActionType, content.TargetSE)
		progress := int32(0)
		this.sendActionResult(proto.ActionResponseState_FAILED, progress, msgID, "Failed")
	}
	// TODO init discovery
}

func (this *ActionHandler) Execute(actionItem *proto.ActionItemDTO, msgID int32) {
	_, err := this.actionExecutor.ExcuteAction(actionItem, msgID)
	if err != nil {
		glog.Errorf("Error execute action: %s", err)
		this.sendActionResult(proto.ActionResponseState_FAILED, int32(0), msgID, "Failed")
	}
}

func (handler *ActionHandler) ResultChan() *proto.ActionResult {
	return <-handler.resultChan
}

// Send action response to vmt server.
func (handler *ActionHandler) sendActionResult(state proto.ActionResponseState, progress, messageID int32, description string) {
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
