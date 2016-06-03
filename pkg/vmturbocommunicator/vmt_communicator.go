package vmturbocommunicator

import (
	// "time"

	// "k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	// "k8s.io/kubernetes/pkg/labels"

	vmtapi "github.com/vmturbo/kubeturbo/pkg/api"
	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	comm "github.com/vmturbo/vmturbo-go-sdk/communicator"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
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
	// 1. Construct the account definition for kubernetes.
	acctDefProps := createAccountDefKubernetes()

	// 2. Build supply chain.
	templateDtos := createSupplyChain()
	glog.V(2).Infof("Supply chain for Kubernetes is created.")

	// 3. construct the kubernetesProbe, the only probe supported.
	probeType := vmtcomm.meta.TargetType
	probeCat := "Container"
	kubernateProbe := comm.NewProbeInfoBuilder(probeType, probeCat, templateDtos, acctDefProps).Create()

	// 4. Add kubernateProbe to probe, and that's the only probe supported in this client.
	var probes []*comm.ProbeInfo
	probes = append(probes, kubernateProbe)

	// 5. Create mediation container
	containerInfo := &comm.ContainerInfo{
		Probes: probes,
	}

	vmtcomm.wsComm.RegisterAndListen(containerInfo)
}

// TODO, rephrase comment.
// create account definition for kubernetes, which is used later to create Kubernetes probe.
// The return type is a list of ProbeInfo_AccountDefProp.
// For a valid definition, targetNameIdentifier, username and password should be contained.
func createAccountDefKubernetes() []*comm.AccountDefEntry {
	var acctDefProps []*comm.AccountDefEntry

	// target id
	targetIDAcctDefEntry := comm.NewAccountDefEntryBuilder("targetIdentifier", "Address",
		"IP of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, false).Create()
	acctDefProps = append(acctDefProps, targetIDAcctDefEntry)

	// username
	usernameAcctDefEntry := comm.NewAccountDefEntryBuilder("username", "Username",
		"Username of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, false).Create()
	acctDefProps = append(acctDefProps, usernameAcctDefEntry)

	// password
	passwdAcctDefEntry := comm.NewAccountDefEntryBuilder("password", "Password",
		"Password of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, true).Create()
	acctDefProps = append(acctDefProps, passwdAcctDefEntry)

	return acctDefProps
}

// also include kubernetes supply chain explanation
func createSupplyChain() []*sdk.TemplateDTO {

	fakeKey := "fake"
	emptyKey := ""

	minionSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	minionSupplyChainNodeBuilder = minionSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_MACHINE).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_VMPM_ACCESS, fakeKey).
		Selling(sdk.CommodityDTO_VCPU, emptyKey).
		Selling(sdk.CommodityDTO_VMEM, emptyKey).
		Selling(sdk.CommodityDTO_APPLICATION, fakeKey).
		Selling(sdk.CommodityDTO_CLUSTER, fakeKey)

	// Pod Supplychain builder
	podSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_CONTAINER_POD).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION, fakeKey)

	cpuAllocationType := sdk.CommodityDTO_CPU_ALLOCATION
	cpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &cpuAllocationType,
	}
	memAllocationType := sdk.CommodityDTO_MEM_ALLOCATION
	memAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &memAllocationType,
	}
	clusterType := sdk.CommodityDTO_CLUSTER
	clusterTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &clusterType,
	}

	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Buys(*cpuAllocationTemplateComm).
		Buys(*memAllocationTemplateComm).
		Buys(*clusterTemplateComm)

	// Application supplychain builder
	appSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_APPLICATION).
		Selling(sdk.CommodityDTO_TRANSACTION, fakeKey)
	// Buys CpuAllocation/MemAllocation from Pod
	appCpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &cpuAllocationType,
	}
	appMemAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &memAllocationType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.Provider(sdk.EntityDTO_CONTAINER_POD, sdk.Provider_LAYERED_OVER).Buys(*appCpuAllocationTemplateComm).Buys(*appMemAllocationTemplateComm)
	// Buys VCpu and VMem from VM
	vCpuType := sdk.CommodityDTO_VCPU
	appVCpu := &sdk.TemplateCommodity{
		// Key:           &fakeKey,
		CommodityType: &vCpuType,
	}
	vMemType := sdk.CommodityDTO_VMEM
	appVMem := &sdk.TemplateCommodity{
		// Key:           &fakeKey,
		CommodityType: &vMemType,
	}
	appCommType := sdk.CommodityDTO_APPLICATION
	appAppComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &appCommType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_HOSTING).Buys(*appVCpu).Buys(*appVMem).Buys(*appAppComm)

	// Application supplychain builder
	vAppSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_APPLICATION)

	transactionType := sdk.CommodityDTO_TRANSACTION

	// Buys CpuAllocation/MemAllocation from Pod
	transactionTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &transactionType,
	}
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.Provider(sdk.EntityDTO_APPLICATION, sdk.Provider_LAYERED_OVER).Buys(*transactionTemplateComm)

	vmpmaccessType := sdk.CommodityDTO_VMPM_ACCESS
	// Link from Pod to VM
	vmPodExtLinkBuilder := sdk.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(sdk.EntityDTO_CONTAINER_POD, sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType, true).
		Commodity(memAllocationType, true).
		Commodity(vmpmaccessType, true).
		Commodity(clusterType, true).
		ProbeEntityPropertyDef(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Pod is running").
		ExternalEntityPropertyDef(sdk.VM_IP)

	vmPodExternalLink := vmPodExtLinkBuilder.Build()

	// Link from Application to VM
	vmAppExtLinkBuilder := sdk.NewExternalEntityLinkBuilder()
	vmAppExtLinkBuilder.Link(sdk.EntityDTO_APPLICATION, sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(appCommType, true).
		ProbeEntityPropertyDef(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Application is running").
		ExternalEntityPropertyDef(sdk.VM_IP)

	vmAppExternalLink := vmAppExtLinkBuilder.Build()

	supplyChainBuilder := sdk.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(appSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmAppExternalLink)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmPodExternalLink)
	supplyChainBuilder.Entity(minionSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
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

// Code is very similar to those in serverMsgHandler.
func (vmtcomm *VMTCommunicator) DiscoverTarget() {
	vmtUrl := vmtcomm.wsComm.VmtServerAddress

	extCongfix := make(map[string]string)
	extCongfix["Username"] = vmtcomm.meta.OpsManagerUsername
	extCongfix["Password"] = vmtcomm.meta.OpsManagerPassword
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Discover Kubernetes target.
	vmturboApi.DiscoverTarget(vmtcomm.meta.NameOrAddress)
}
