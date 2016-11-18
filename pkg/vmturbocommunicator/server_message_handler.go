package vmturbocommunicator

import (
	"net"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util/wait"

	vmtaction "github.com/vmturbo/kubeturbo/pkg/action"
	vmtapi "github.com/vmturbo/kubeturbo/pkg/api"
	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
	"github.com/vmturbo/kubeturbo/pkg/probe"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	comm "github.com/vmturbo/vmturbo-go-sdk/pkg/communication"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// impletements sdk.ServerMessageHandler
type KubernetesServerMessageHandler struct {
	kubeClient  *client.Client
	meta        *vmtmeta.VMTMeta
	wsComm      *comm.WebSocketCommunicator
	etcdStorage storage.Storage
	probeConfig *probe.ProbeConfig

	actionHandler *vmtaction.ActionHandler
}

func NewKubernetesServerMessageHandler(kubeClient *client.Client, meta *vmtmeta.VMTMeta,
	wsCommunicator *comm.WebSocketCommunicator, etcdStorage storage.Storage,
	pConfig *probe.ProbeConfig) *KubernetesServerMessageHandler {

	actionHandlerConfig := vmtaction.NewActionHandlerConfig(kubeClient, etcdStorage, wsCommunicator)
	actionHandler := vmtaction.NewActionHandler(actionHandlerConfig)

	return &KubernetesServerMessageHandler{
		kubeClient:  kubeClient,
		meta:        meta,
		wsComm:      wsCommunicator,
		etcdStorage: etcdStorage,
		probeConfig: pConfig,

		actionHandler: actionHandler,
	}
}

func (handler *KubernetesServerMessageHandler) StartActionHandler() {
	handler.actionHandler.Start()
}

func (handler *KubernetesServerMessageHandler) Callback() <-chan *proto.MediationClientMessage {
	return nil
}

// Use the vmt restAPI to add a Kubernetes target.
func (handler *KubernetesServerMessageHandler) AddTarget() {
	vmtUrl := net.JoinHostPort(handler.meta.ServerAddress, handler.meta.ServerAPIPort)

	extCongfix := make(map[string]string)
	extCongfix["Username"] = handler.meta.OpsManagerUsername
	extCongfix["Password"] = handler.meta.OpsManagerPassword
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Add Kubernetes target.
	// targetType, nameOrAddress, targetIdentifier, password
	vmturboApi.AddK8sTarget(handler.meta.TargetType, handler.meta.NameOrAddress, handler.meta.Username, handler.meta.TargetIdentifier, handler.meta.Password)
}

// Send an API request to make server start a discovery process on current k8s.
func (handler *KubernetesServerMessageHandler) DiscoverTarget() {
	vmtUrl := net.JoinHostPort(handler.meta.ServerAddress, handler.meta.ServerAPIPort)

	extCongfix := make(map[string]string)
	extCongfix["Username"] = handler.meta.OpsManagerUsername
	extCongfix["Password"] = handler.meta.OpsManagerPassword
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Discover Kubernetes target.
	vmturboApi.DiscoverTarget(handler.meta.NameOrAddress)
}

// If server sends a validation request, validate the request.
// TODO, for now k8s validate all the request. aka, no matter what usr/passwd is provided, always pass validation.
// The correct bahavior is to set ErrorDTO when validation fails.
func (handler *KubernetesServerMessageHandler) Validate(serverMsg *proto.MediationServerMessage) {
	//Always send Validated for now
	glog.V(3).Infof("Kubernetes validation request from Server")

	// 1. Get message ID.
	messageID := serverMsg.GetMessageID()
	// 2. Build validationResponse.
	validationResponse := new(proto.ValidationResponse)
	// 3. Create client message with ClientMessageBuilder.
	clientMsg := comm.NewClientMessageBuilder(messageID).SetValidationResponse(validationResponse).Create()
	handler.wsComm.SendClientMessage(clientMsg)

	// TODO: Need to sleep some time, waiting validated. Or we should add reponse msg from server.
	time.Sleep(100 * time.Millisecond)
	glog.V(2).Infof("Discovery Target after validation")

	handler.DiscoverTarget()
}

func (handler *KubernetesServerMessageHandler) keepDiscoverAlive(messageID int32) {
	//
	glog.V(3).Infof("Keep Alive")

	keepAliveMsg := &proto.KeepAlive{}
	clientMsg := comm.NewClientMessageBuilder(messageID).SetKeepAlive(keepAliveMsg).Create()

	handler.wsComm.SendClientMessage(clientMsg)
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
func (handler *KubernetesServerMessageHandler) DiscoverTopology(serverMsg *proto.MediationServerMessage) {
	//Discover the kubernetes topology
	glog.V(3).Infof("Discover topology request from server.")

	// 1. Get message ID
	messageID := serverMsg.GetMessageID()
	var stopCh chan struct{} = make(chan struct{})
	go wait.Until(func() { handler.keepDiscoverAlive(messageID) }, time.Second*10, stopCh)
	defer close(stopCh)

	// 2. Build discoverResponse
	// must have kubeClient to do ParseNode and ParsePod
	if handler.kubeClient == nil {
		glog.Errorf("kubenetes client is nil, error")
		return
	}

	kubeProbe := probe.NewKubeProbe(handler.kubeClient, handler.probeConfig)

	nodeEntityDtos, err := kubeProbe.ParseNode()
	if err != nil {
		// TODO, should here still send out msg to server?
		glog.Errorf("Error parsing nodes: %s. Will return.", err)
		return
	}

	podEntityDtos, err := kubeProbe.ParsePod(api.NamespaceAll)
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		glog.Errorf("Error parsing pods: %s. Will return.", err)
		return
	}

	appEntityDtos, err := kubeProbe.ParseApplication(api.NamespaceAll)
	if err != nil {
		glog.Errorf("Error parsing applications: %s. Will return.", err)
		return
	}

	serviceEntityDtos, err := kubeProbe.ParseService(api.NamespaceAll, labels.Everything())
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		glog.Errorf("Error parsing services: %s. Will return.", err)
		return
	}

	entityDtos := nodeEntityDtos
	entityDtos = append(entityDtos, podEntityDtos...)
	entityDtos = append(entityDtos, appEntityDtos...)
	entityDtos = append(entityDtos, serviceEntityDtos...)
	discoveryResponse := &proto.DiscoveryResponse{
		EntityDTO: entityDtos,
	}

	// 3. Build Client message
	clientMsg := comm.NewClientMessageBuilder(messageID).SetDiscoveryResponse(discoveryResponse).Create()

	handler.wsComm.SendClientMessage(clientMsg)
}

// Receives an action request from server and call ActionExecutor to execute action.
func (handler *KubernetesServerMessageHandler) HandleAction(serverMsg *proto.MediationServerMessage) {
	messageID := serverMsg.GetMessageID()
	actionRequest := serverMsg.GetActionRequest()
	actionExectionDTO := actionRequest.GetActionExecutionDTO()
	actionItems := actionExectionDTO.GetActionItem()
	actionItemDTO := actionItems[0]
	glog.V(4).Infof("The received ActionItemDTO is %v", actionItemDTO)

	handler.actionHandler.Execute(actionItemDTO, messageID)

	result := handler.actionHandler.ResultChan()

	clientMsg := comm.NewClientMessageBuilder(messageID).SetActionResponse(result).Create()

	handler.wsComm.SendClientMessage(clientMsg)

	time.Sleep(time.Millisecond * 500)
	handler.DiscoverTarget()
}
