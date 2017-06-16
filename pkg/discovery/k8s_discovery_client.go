package discovery

import (
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"
	kubeClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// TODO maybe use a discovery client config
type DiscoveryConfig struct {
	kubeClient  *kubeClient.Clientset
	probeConfig *probe.ProbeConfig

	targetConfig *K8sTargetConfig
}

func NewDiscoveryConfig(kubeCli *kubeClient.Clientset, probeConfig *probe.ProbeConfig, targetConfig *K8sTargetConfig) *DiscoveryConfig {
	return &DiscoveryConfig{
		kubeClient:   kubeCli,
		probeConfig:  probeConfig,
		targetConfig: targetConfig,
	}
}

type K8sDiscoveryClient struct {
	config *DiscoveryConfig
}

func NewK8sDiscoveryClient(config *DiscoveryConfig) *K8sDiscoveryClient {
	return &K8sDiscoveryClient{
		config: config,
	}
}

func (dc *K8sDiscoveryClient) GetAccountValues() *sdkprobe.TurboTargetInfo {
	var accountValues []*proto.AccountValue
	targetConf := dc.config.targetConfig
	// Convert all parameters in clientConf to AccountValue list
	targetID := registration.TargetIdentifierField
	accVal := &proto.AccountValue{
		Key:         &targetID,
		StringValue: &targetConf.TargetIdentifier,
	}
	accountValues = append(accountValues, accVal)

	username := registration.Username
	accVal = &proto.AccountValue{
		Key:         &username,
		StringValue: &targetConf.TargetUsername,
	}
	accountValues = append(accountValues, accVal)

	password := registration.Password
	accVal = &proto.AccountValue{
		Key:         &password,
		StringValue: &targetConf.TargetPassword,
	}
	accountValues = append(accountValues, accVal)

	targetInfo := sdkprobe.NewTurboTargetInfoBuilder(targetConf.ProbeCategory, targetConf.TargetType, targetID, accountValues).Create()
	return targetInfo
}

// Validate the Target
func (dc *K8sDiscoveryClient) Validate(accountValues []*proto.AccountValue) (*proto.ValidationResponse, error) {
	glog.V(2).Infof("Validating Kubernetes target...")

	// TODO: connect to the client and get validation response
	validationResponse := &proto.ValidationResponse{}

	return validationResponse, nil
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
func (dc *K8sDiscoveryClient) Discover(accountValues []*proto.AccountValue) (*proto.DiscoveryResponse, error) {
	//Discover the Kubernetes topology
	glog.V(2).Infof("Discovering Kubernetes cluster...")

	// must have kubeClient to do ParseNode and ParsePod
	if dc.config.kubeClient == nil {
		// TODO make error dto
		return nil, fmt.Errorf("Kubenetes client is nil, error")
	}

	kubeProbe, err := probe.NewKubeProbe(dc.config.kubeClient, dc.config.probeConfig)
	if err != nil {
		// TODO make error dto
		return nil, fmt.Errorf("Error creating Kubernetes discovery probe:%s", err.Error())
	}

	nodeEntityDtos, err := kubeProbe.ParseNode()
	if err != nil {
		// TODO make error dto
		return nil, fmt.Errorf("Error parsing nodes: %s. Will return.", err)
	}

	podEntityDtos, err := kubeProbe.ParsePod(api.NamespaceAll)
	if err != nil {
		glog.Errorf("Error parsing pods: %s. Skip.", err)
		// TODO make error dto
	}

	appEntityDtos, err := kubeProbe.ParseApplication(api.NamespaceAll)
	if err != nil {
		glog.Errorf("Error parsing applications: %s. Skip.", err)
	}

	serviceEntityDtos, err := kubeProbe.ParseService(api.NamespaceAll)
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		glog.Errorf("Error parsing services: %s. Skip.", err)
	}

	entityDtos := nodeEntityDtos
	entityDtos = append(entityDtos, podEntityDtos...)
	entityDtos = append(entityDtos, appEntityDtos...)
	entityDtos = append(entityDtos, serviceEntityDtos...)
	discoveryResponse := &proto.DiscoveryResponse{
		EntityDTO: entityDtos,
	}

	return discoveryResponse, nil
}
