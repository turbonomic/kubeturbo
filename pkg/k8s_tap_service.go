package kubeturbo

import (
	"fmt"

	//	"github.com/turbonomic/turbo-go-sdk/pkg/communication"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"

	"github.com/vmturbo/kubeturbo/pkg/discovery"
	"github.com/vmturbo/kubeturbo/pkg/registration"
)

type K8sTAPServiceConfig struct {
	communicationContainerConfigPath string

	probeCategory string
	targetType    string
	targetID      string

	discoveryClientConfig *discovery.DiscoveryConfig
}

func NewK8sTAPServiceConfig(communicationContainerConfigPath, probeCategory, targetType, targetID string,
	discoveryClientConfig *discovery.DiscoveryConfig) *K8sTAPServiceConfig {
	return &K8sTAPServiceConfig{
		communicationContainerConfigPath: communicationContainerConfigPath,
		probeCategory:                    probeCategory,
		targetType:                       targetType,
		targetID:                         targetID,

		discoveryClientConfig: discoveryClientConfig,
	}
}

type K8sTAPService struct {
	*service.TAPService
}

func NewKubernetesTAPService(config *K8sTAPServiceConfig) (*K8sTAPService, error) {
	// Kubernetes Probe Registration Client
	registrationClient := registration.NewK8sRegistrationClient()
	// Kubernetes Probe Discovery Client
	discoveryClient := discovery.NewK8sDiscoveryClient(config.discoveryClientConfig)

	communicationConfig, err := service.ParseTurboCommunicationConfig(config.communicationContainerConfigPath)
	if err != nil {
		return nil, fmt.Errorf("Error parsing communication config: %s", err)
	}

	tapService, err :=
		service.NewTAPServiceBuilder().
			WithTurboCommunicator(communicationConfig).
			WithTurboProbe(probe.NewProbeBuilder(config.targetType, config.probeCategory).
				RegisteredBy(registrationClient).
				DiscoversTarget(config.targetID, discoveryClient)).
			Create()
	if err != nil {
		return nil, fmt.Errorf("Error when creating KubernetesTAPService: %s", err)
	}

	//// Connect to the Turbo server
	//tapService.ConnectToTurbo()
	return &K8sTAPService{tapService}, nil
}
