package registration

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	TargetIdentifierField string = "targetIdentifier"
	Username              string = "username"
	Password              string = "password"
)

type RegistrationConfig struct {
	// The property used for stitching.
	stitchingPropertyType stitching.StitchingPropertyType
}

func NewRegistrationClientConfig(pType stitching.StitchingPropertyType) *RegistrationConfig {
	return &RegistrationConfig{
		stitchingPropertyType: pType,
	}
}

type K8sRegistrationClient struct {
	config *RegistrationConfig
}

func NewK8sRegistrationClient(config *RegistrationConfig) *K8sRegistrationClient {
	return &K8sRegistrationClient{
		config: config,
	}
}

func (rClient *K8sRegistrationClient) GetSupplyChainDefinition() []*proto.TemplateDTO {
	supplyChainFactory := NewSupplyChainFactory(rClient.config.stitchingPropertyType)
	supplyChain, err := supplyChainFactory.createSupplyChain()
	if err != nil {
		// TODO error handling
	}
	return supplyChain
}

func (rClient *K8sRegistrationClient) GetAccountDefinition() []*proto.AccountDefEntry {
	var acctDefProps []*proto.AccountDefEntry

	// target ID
	targetIDAcctDefEntry := builder.NewAccountDefEntryBuilder(TargetIdentifierField, "Address",
		"IP of the Kubernetes master", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, targetIDAcctDefEntry)

	// username
	usernameAcctDefEntry := builder.NewAccountDefEntryBuilder(Username, "Username",
		"Username of the Kubernetes master", ".*", false, false).Create()
	acctDefProps = append(acctDefProps, usernameAcctDefEntry)

	// password
	passwordAcctDefEntry := builder.NewAccountDefEntryBuilder(Password, "Password",
		"Password of the Kubernetes master", ".*", false, true).Create()
	acctDefProps = append(acctDefProps, passwordAcctDefEntry)

	return acctDefProps
}

func (rClient *K8sRegistrationClient) GetIdentifyingFields() string {
	return TargetIdentifierField
}
