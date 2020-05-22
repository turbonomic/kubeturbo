package registration

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	TargetIdentifierField string = "targetIdentifier"
	MasterHost            string = "masterHost"
	ServerVersion         string = "serverVersion"
	Image                 string = "image"
	ImageID               string = "imageID"
	propertyId            string = "id"
)

type RegistrationConfig struct {
	// The property used for stitching.
	stitchingPropertyType stitching.StitchingPropertyType
	vmPriority            int32
	vmIsBase              bool
}

func NewRegistrationClientConfig(pType stitching.StitchingPropertyType, p int32, isbase bool) *RegistrationConfig {
	return &RegistrationConfig{
		stitchingPropertyType: pType,
		vmPriority:            p,
		vmIsBase:              isbase,
	}
}

type K8sRegistrationClient struct {
	config       *RegistrationConfig
	targetConfig *configs.K8sTargetConfig
}

func NewK8sRegistrationClient(config *RegistrationConfig, targetConfig *configs.K8sTargetConfig) *K8sRegistrationClient {
	return &K8sRegistrationClient{
		config:       config,
		targetConfig: targetConfig,
	}
}

func (rClient *K8sRegistrationClient) GetSupplyChainDefinition() []*proto.TemplateDTO {
	supplyChainFactory := NewSupplyChainFactory(rClient.config.stitchingPropertyType, rClient.config.vmPriority, rClient.config.vmIsBase)
	supplyChain, err := supplyChainFactory.createSupplyChain()
	if err != nil {
		glog.Errorf("Failed to create supply chain: %v", err)
		// TODO error handling
	}
	return supplyChain
}

func (rClient *K8sRegistrationClient) GetAccountDefinition() (acctDefProps []*proto.AccountDefEntry) {
	// Target ID
	targetIDAcctDefEntry := builder.NewAccountDefEntryBuilder(TargetIdentifierField, "Name",
		"Name of the Kubernetes cluster", ".*", true, false).Create()
	acctDefProps = append(acctDefProps, targetIDAcctDefEntry)

	// Do not register the following account definitions if no target has been defined
	// in kubeturbo configuration. The target will be added manually.
	if rClient.targetConfig.TargetIdentifier == "" {
		return
	}

	// Register the following account definitions if a target has been defined and added by kubeturbo.
	// These fields are meant for read only to provide more information of the probe and target:
	// Master Host
	masterHostEntry := builder.NewAccountDefEntryBuilder(MasterHost, "Master Host",
		"Address of the Kubernetes master", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, masterHostEntry)
	// Kubernetes Server Version
	serverVersion := builder.NewAccountDefEntryBuilder(ServerVersion, "Kubernetes Server Version",
		"Version of the Kubernetes server", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, serverVersion)
	// Probe Container Image
	image := builder.NewAccountDefEntryBuilder(Image, "Kubeturbo Image",
		"Container Image of Kubeturbo Probe", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, image)
	// Probe Container Image ID
	imageID := builder.NewAccountDefEntryBuilder(ImageID, "Kubeturbo Image ID",
		"Container Image ID of Kubeturbo Probe", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, imageID)

	return
}

func (rClient *K8sRegistrationClient) GetIdentifyingFields() string {
	return TargetIdentifierField
}

func (rClient *K8sRegistrationClient) GetActionPolicy() []*proto.ActionPolicyDTO {
	glog.V(3).Infof("Begin to build Action Policies")
	ab := builder.NewActionPolicyBuilder()
	supported := proto.ActionPolicyDTO_SUPPORTED
	recommend := proto.ActionPolicyDTO_NOT_EXECUTABLE
	notSupported := proto.ActionPolicyDTO_NOT_SUPPORTED

	// 1. containerPod: support move, provision and suspend; not resize;
	pod := proto.EntityDTO_CONTAINER_POD
	podPolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	podPolicy[proto.ActionItemDTO_MOVE] = supported
	podPolicy[proto.ActionItemDTO_PROVISION] = supported
	podPolicy[proto.ActionItemDTO_RIGHT_SIZE] = notSupported
	podPolicy[proto.ActionItemDTO_SUSPEND] = supported

	rClient.addActionPolicy(ab, pod, podPolicy)

	// 2. container: support resize; recommend provision and suspend; not move;
	container := proto.EntityDTO_CONTAINER
	containerPolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	containerPolicy[proto.ActionItemDTO_RIGHT_SIZE] = supported
	containerPolicy[proto.ActionItemDTO_PROVISION] = recommend
	containerPolicy[proto.ActionItemDTO_MOVE] = notSupported
	containerPolicy[proto.ActionItemDTO_SUSPEND] = recommend

	rClient.addActionPolicy(ab, container, containerPolicy)

	// 3. application: only recommend provision and suspend; all else are not supported
	app := proto.EntityDTO_APPLICATION
	appPolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	appPolicy[proto.ActionItemDTO_PROVISION] = recommend
	appPolicy[proto.ActionItemDTO_RIGHT_SIZE] = notSupported
	appPolicy[proto.ActionItemDTO_MOVE] = notSupported
	appPolicy[proto.ActionItemDTO_SUSPEND] = recommend

	rClient.addActionPolicy(ab, app, appPolicy)

	// 4. virtual application: no actions are supported
	vApp := proto.EntityDTO_VIRTUAL_APPLICATION
	vAppPolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	vAppPolicy[proto.ActionItemDTO_PROVISION] = notSupported
	vAppPolicy[proto.ActionItemDTO_RIGHT_SIZE] = notSupported
	vAppPolicy[proto.ActionItemDTO_MOVE] = notSupported
	vAppPolicy[proto.ActionItemDTO_SUSPEND] = notSupported

	rClient.addActionPolicy(ab, vApp, vAppPolicy)

	// 5. node: support provision and suspend; not resize; do not set move
	node := proto.EntityDTO_VIRTUAL_MACHINE
	nodePolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	nodePolicy[proto.ActionItemDTO_PROVISION] = supported
	nodePolicy[proto.ActionItemDTO_RIGHT_SIZE] = notSupported
	nodePolicy[proto.ActionItemDTO_SCALE] = notSupported
	nodePolicy[proto.ActionItemDTO_SUSPEND] = supported

	rClient.addActionPolicy(ab, node, nodePolicy)

	// 6. workload controller: support  resize
	controller := proto.EntityDTO_WORKLOAD_CONTROLLER
	controllerPolicy := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	controllerPolicy[proto.ActionItemDTO_RIGHT_SIZE] = supported

	rClient.addActionPolicy(ab, controller, controllerPolicy)

	return ab.Create()
}

func (rClient *K8sRegistrationClient) addActionPolicy(ab *builder.ActionPolicyBuilder,
	entity proto.EntityDTO_EntityType,
	policies map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability) {

	for action, policy := range policies {
		ab.WithEntityActions(entity, action, policy)
	}
}

func (rClient *K8sRegistrationClient) GetEntityMetadata() (result []*proto.EntityIdentityMetadata) {
	glog.V(3).Infof("Begin to build EntityIdentityMetadata")

	entities := []proto.EntityDTO_EntityType{
		proto.EntityDTO_NAMESPACE,
		proto.EntityDTO_WORKLOAD_CONTROLLER,
		proto.EntityDTO_VIRTUAL_MACHINE,
		proto.EntityDTO_CONTAINER_SPEC,
		proto.EntityDTO_CONTAINER_POD,
		proto.EntityDTO_CONTAINER,
		proto.EntityDTO_APPLICATION,
		proto.EntityDTO_VIRTUAL_APPLICATION,
	}

	for _, etype := range entities {
		meta := rClient.newIdMetaData(etype, []string{propertyId})
		result = append(result, meta)
	}

	glog.V(4).Infof("EntityIdentityMetaData: %++v", result)

	return result
}

func (rClient *K8sRegistrationClient) newIdMetaData(etype proto.EntityDTO_EntityType, names []string) *proto.EntityIdentityMetadata {
	var data []*proto.EntityIdentityMetadata_PropertyMetadata
	for _, name := range names {
		dat := &proto.EntityIdentityMetadata_PropertyMetadata{
			Name: &name,
		}
		data = append(data, dat)
	}

	result := &proto.EntityIdentityMetadata{
		EntityType:            &etype,
		NonVolatileProperties: data,
	}

	return result
}
