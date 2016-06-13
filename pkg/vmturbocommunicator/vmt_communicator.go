package vmturbocommunicator

import (
	client "k8s.io/kubernetes/pkg/client/unversioned"

	vmtapi "github.com/vmturbo/kubeturbo/pkg/api"
	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
	"github.com/vmturbo/kubeturbo/pkg/storage"
	external "github.com/vmturbo/kubeturbo/pkg/vmturbocommunicator/externalprobebuilder"

	comm "github.com/vmturbo/vmturbo-go-sdk/communicator"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

type VMTCommunicator struct {
	kubeClient  *client.Client
	meta        *vmtmeta.VMTMeta
	wsComm      *comm.WebSocketCommunicator
	etcdStorage storage.Storage
}

func NewVMTCommunicator(client *client.Client, vmtMetadata *vmtmeta.VMTMeta, storage storage.Storage) *VMTCommunicator {
	return &VMTCommunicator{
		kubeClient:  client,
		meta:        vmtMetadata,
		etcdStorage: storage,
	}
}

func (vmtcomm *VMTCommunicator) Run() {
	vmtcomm.Init()
	vmtcomm.RegisterKubernetes()
}

// Init() intialize the VMTCommunicator, creating websocket communicator and server message handler.
func (vmtcomm *VMTCommunicator) Init() {
	wsCommunicator := &comm.WebSocketCommunicator{
		VmtServerAddress: vmtcomm.meta.ServerAddress,
		LocalAddress:     vmtcomm.meta.LocalAddress,
		ServerUsername:   vmtcomm.meta.WebSocketUsername,
		ServerPassword:   vmtcomm.meta.WebSocketPassword,
	}
	vmtcomm.wsComm = wsCommunicator

	// First create the message handler for kubernetes
	kubeMsgHandler := &KubernetesServerMessageHandler{
		kubeClient:  vmtcomm.kubeClient,
		meta:        vmtcomm.meta,
		wsComm:      wsCommunicator,
		etcdStorage: vmtcomm.etcdStorage,
	}
	wsCommunicator.ServerMsgHandler = kubeMsgHandler
	return
}

// Register Kubernetes target onto server and start listen to websocket.
func (vmtcomm *VMTCommunicator) RegisterKubernetes() {
	externalProbeBuilder := &external.ExternalProbeBuilder{}
	probes := externalProbeBuilder.BuildProbes(vmtcomm.meta.TargetType)

	// Create mediation container
	containerInfo := &comm.ContainerInfo{
		Probes: probes,
	}

	vmtcomm.wsComm.RegisterAndListen(containerInfo)
}

// Send action response to vmt server.
func (vmtcomm *VMTCommunicator) SendActionReponse(state sdk.ActionResponseState, progress, messageID int32, description string) {
	// 1. build response
	response := &comm.ActionResponse{
		ActionResponseState: &state,
		Progress:            &progress,
		ResponseDescription: &description,
	}

	// 2. built action result.
	result := &comm.ActionResult{
		Response: response,
	}

	// 3. Build Client message
	clientMsg := comm.NewClientMessageBuilder(messageID).SetActionResponse(result).Create()

	vmtcomm.wsComm.SendClientMessage(clientMsg)
}

// Call VMTurbo REST API to initialize a discovery.
func (vmtcomm *VMTCommunicator) DiscoverTarget() {
	vmtUrl := vmtcomm.wsComm.VmtServerAddress

	extCongfix := make(map[string]string)
	extCongfix["Username"] = vmtcomm.meta.OpsManagerUsername
	extCongfix["Password"] = vmtcomm.meta.OpsManagerPassword
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Discover Kubernetes target.
	vmturboApi.DiscoverTarget(vmtcomm.meta.NameOrAddress)
}
