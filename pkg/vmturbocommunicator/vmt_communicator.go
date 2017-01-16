package vmturbocommunicator
//
//import (
//	"net"
//
//	client "k8s.io/kubernetes/pkg/client/unversioned"
//
//	vmtapi "github.com/vmturbo/kubeturbo/pkg/api"
//	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
//	"github.com/vmturbo/kubeturbo/pkg/probe"
//	"github.com/vmturbo/kubeturbo/pkg/storage"
//	external "github.com/vmturbo/kubeturbo/pkg/vmturbocommunicator/externalprobebuilder"
//
//	comm "github.com/vmturbo/vmturbo-go-sdk/pkg/communication"
//	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"
//
//	"github.com/golang/glog"
//)
//
//type VMTCommunicator struct {
//	kubeClient  *client.Client
//	meta        *vmtmeta.VMTMeta
//	wsComm      *comm.WebSocketCommunicator
//	etcdStorage storage.Storage
//	probeConifg *probe.ProbeConfig
//}
//
//func NewVMTCommunicator(client *client.Client, vmtMetadata *vmtmeta.VMTMeta, storage storage.Storage, pConfig *probe.ProbeConfig) *VMTCommunicator {
//	return &VMTCommunicator{
//		kubeClient:  client,
//		meta:        vmtMetadata,
//		etcdStorage: storage,
//		probeConifg: pConfig,
//	}
//}
//
//func (vmtcomm *VMTCommunicator) Run() {
//	vmtcomm.Init()
//	vmtcomm.RegisterKubernetes()
//}
//
//// Init() intialize the VMTCommunicator, creating websocket communicator and server message handler.
//func (vmtcomm *VMTCommunicator) Init() {
//	serverWebSocketAddress := vmtcomm.meta.ServerAddress
//	glog.Info("Websocket Port is %s", vmtcomm.meta.WebSocketPort)
//	if vmtcomm.meta.WebSocketPort != "" {
//		serverWebSocketAddress = net.JoinHostPort(serverWebSocketAddress, vmtcomm.meta.WebSocketPort)
//	}
//	wsCommunicator := &comm.WebSocketCommunicator{
//		VmtServerAddress: serverWebSocketAddress,
//		LocalAddress:     vmtcomm.meta.LocalAddress,
//		ServerUsername:   vmtcomm.meta.WebSocketUsername,
//		ServerPassword:   vmtcomm.meta.WebSocketPassword,
//	}
//	vmtcomm.wsComm = wsCommunicator
//
//	// First create the message handler for kubernetes
//	kubeMsgHandler := NewKubernetesServerMessageHandler(vmtcomm.kubeClient,
//		vmtcomm.meta, wsCommunicator, vmtcomm.etcdStorage, vmtcomm.probeConifg)
//	kubeMsgHandler.StartActionHandler()
//
//	wsCommunicator.ServerMsgHandler = kubeMsgHandler
//	return
//}
//
//// Register Kubernetes target onto server and start listen to websocket.
//func (vmtcomm *VMTCommunicator) RegisterKubernetes() error {
//	externalProbeBuilder := &external.ExternalProbeBuilder{}
//	probes, err := externalProbeBuilder.BuildProbes(vmtcomm.meta.TargetType)
//	if err != nil {
//		return err
//	}
//
//	// Create mediation container
//	containerInfo := &proto.ContainerInfo{
//		Probes: probes,
//	}
//
//	vmtcomm.wsComm.RegisterAndListen(containerInfo)
//
//	return nil
//}
//
//// Send action response to vmt server.
//func (vmtcomm *VMTCommunicator) SendActionReponse(state proto.ActionResponseState, progress, messageID int32, description string) {
//	// 1. build response
//	response := &proto.ActionResponse{
//		ActionResponseState: &state,
//		Progress:            &progress,
//		ResponseDescription: &description,
//	}
//
//	// 2. built action result.
//	result := &proto.ActionResult{
//		Response: response,
//	}
//
//	// 3. Build Client message
//	clientMsg := comm.NewClientMessageBuilder(messageID).SetActionResponse(result).Create()
//
//	vmtcomm.wsComm.SendClientMessage(clientMsg)
//}
//
//// Call VMTurbo REST API to initialize a discovery.
//func (vmtcomm *VMTCommunicator) DiscoverTarget() {
//	vmtUrl := net.JoinHostPort(vmtcomm.meta.ServerAddress, vmtcomm.meta.ServerAPIPort)
//
//	extCongfix := make(map[string]string)
//	extCongfix["Username"] = vmtcomm.meta.OpsManagerUsername
//	extCongfix["Password"] = vmtcomm.meta.OpsManagerPassword
//	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)
//
//	// Discover Kubernetes target.
//	vmturboApi.DiscoverTarget(vmtcomm.meta.NameOrAddress)
//}
