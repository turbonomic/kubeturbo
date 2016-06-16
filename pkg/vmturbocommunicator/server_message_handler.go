package vmturbocommunicator

import (
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

	comm "github.com/vmturbo/vmturbo-go-sdk/communicator"

	"github.com/golang/glog"
)

// impletements sdk.ServerMessageHandler
type KubernetesServerMessageHandler struct {
	kubeClient  *client.Client
	meta        *vmtmeta.VMTMeta
	wsComm      *comm.WebSocketCommunicator
	etcdStorage storage.Storage
}

// Use the vmt restAPI to add a Kubernetes target.
func (handler *KubernetesServerMessageHandler) AddTarget() {
	vmtUrl := handler.wsComm.VmtServerAddress

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
	vmtUrl := handler.wsComm.VmtServerAddress

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
func (handler *KubernetesServerMessageHandler) Validate(serverMsg *comm.MediationServerMessage) {
	//Always send Validated for now
	glog.V(3).Infof("Kubernetes validation request from Server")

	// 1. Get message ID.
	messageID := serverMsg.GetMessageID()
	// 2. Build validationResponse.
	validationResponse := new(comm.ValidationResponse)
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

	keepAliveMsg := &comm.KeepAlive{}
	clientMsg := comm.NewClientMessageBuilder(messageID).SetKeepAlive(keepAliveMsg).Create()

	handler.wsComm.SendClientMessage(clientMsg)
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
func (handler *KubernetesServerMessageHandler) DiscoverTopology(serverMsg *comm.MediationServerMessage) {
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

	kubeProbe := probe.NewKubeProbe(handler.kubeClient)

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
	discoveryResponse := &comm.DiscoveryResponse{
		EntityDTO: entityDtos,
	}

	// 3. Build Client message
	clientMsg := comm.NewClientMessageBuilder(messageID).SetDiscoveryResponse(discoveryResponse).Create()

	handler.wsComm.SendClientMessage(clientMsg)
}

// Receives an action request from server and call ActionExecutor to execute action.
func (handler *KubernetesServerMessageHandler) HandleAction(serverMsg *comm.MediationServerMessage) {
	messageID := serverMsg.GetMessageID()
	actionRequest := serverMsg.GetActionRequest()
	// In the kubernetes case, ProbeType and AccountValue check is not necessary here since
	// the mediation client (vmturbo service) is embeded inside kubernetes.
	actionItemDTO := actionRequest.GetActionItemDTO()
	glog.V(4).Infof("The received ActionItemDTO is %v", actionItemDTO)
	actionExecutor := vmtaction.NewVMTActionExecutor(handler.kubeClient, handler.etcdStorage)
	err := actionExecutor.ExcuteAction(actionItemDTO, messageID)
	if err != nil {
		glog.Errorf("Error execute action: %s", err)
	}
}
