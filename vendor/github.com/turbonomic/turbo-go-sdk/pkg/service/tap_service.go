package service

import (
	"fmt"
	"net/url"

	restclient "github.com/turbonomic/turbo-api/pkg/client"

	"github.com/turbonomic/turbo-go-sdk/pkg/mediationcontainer"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"

	"github.com/golang/glog"
)

const (
	defaultTurboAPIPath = "/vmturbo/rest"
)

// Turbonomic Automation Service that will discover the environment for the Turbo server
// and receive recommendations to control the infrastructure environment
type TAPService struct {
	// TurboProbe provides the communication interface between the Turbo server
	// and the infrastructure environment
	*probe.TurboProbe
	*restclient.Client
	disconnectFromTurbo chan struct{}
}

func (tapService *TAPService) DisconnectFromTurbo() {
	glog.V(4).Infof("[DisconnectFromTurbo] Enter *******")
	close(tapService.disconnectFromTurbo)
	glog.V(4).Infof("[DisconnectFromTurbo] End *********")
}

func (tapService *TAPService) ConnectToTurbo() {
	glog.V(4).Infof("[ConnectToTurbo] Enter ******* ")
	IsRegistered := make(chan bool, 1)

	// start a separate go routine to connect to the Turbo server
	go mediationcontainer.InitMediationContainer(IsRegistered)

	// Wait for probe registration complete to create targets in turbo server
	// Block till a message arrives on the channel
	status := <-IsRegistered
	if !status {
		glog.Fatalf("Probe " + tapService.ProbeConfiguration.ProbeCategory +
			"::" + tapService.ProbeConfiguration.ProbeType + " is not registered")
		return
	}
	glog.V(3).Infof("Probe " + tapService.ProbeConfiguration.ProbeCategory +
		"::" + tapService.ProbeConfiguration.ProbeType + " Registered : === Add Targets ===")

	// Register the probe targets
	targetInfos := tapService.GetProbeTargets()
	for _, targetInfo := range targetInfos {
		target := targetInfo.GetTargetInstance()
		glog.V(4).Infof("Now adding target %v", target)
		resp, err := tapService.AddTarget(target)
		if err != nil {
			glog.Errorf("Error while adding %s %s target: %s", targetInfo.TargetCategory(),
				targetInfo.TargetType(), err)
			// TODO, do we want to return an error?
			continue
		}
		glog.V(3).Infof("Successfully add target: %v", resp)
	}

	close(IsRegistered)
	glog.V(4).Infof("[ConnectToTurbo] End ******")

	// Once connected the mediation container will keep running till a disconnect message is sent to the tap service
	tapService.disconnectFromTurbo = make(chan struct{})
	select {
	case <-tapService.disconnectFromTurbo:
		mediationcontainer.CloseMediationContainer()
		return
	}
}

// ==============================================================================

// Convenience builder for building a TAPService
type TAPServiceBuilder struct {
	tapService *TAPService

	err error
}

// Get an instance of TAPServiceBuilder
func NewTAPServiceBuilder() *TAPServiceBuilder {
	serviceBuilder := &TAPServiceBuilder{}
	service := &TAPService{}
	serviceBuilder.tapService = service
	return serviceBuilder
}

// Build a new instance of TAPService.
func (builder *TAPServiceBuilder) Create() (*TAPService, error) {
	if builder.err != nil {
		return nil, builder.err
	}

	return builder.tapService, nil
}

// Build the mediation container and Turbo API client.
func (builder *TAPServiceBuilder) WithTurboCommunicator(commConfig *TurboCommunicationConfig) *TAPServiceBuilder {
	if builder.err != nil {
		return builder
	}

	// Create the mediation container. This is a singleton.
	containerConfig := &mediationcontainer.MediationContainerConfig{
		ServerMeta:      commConfig.ServerMeta,
		WebSocketConfig: commConfig.WebSocketConfig,
	}
	mediationcontainer.CreateMediationContainer(containerConfig)

	// The RestAPI Handler
	serverAddress, err := url.Parse(commConfig.TurboServer)
	if err != nil {
		builder.err = fmt.Errorf("Error during create Turbo API client config: Incorrect URL: %s\n", err)
		return builder
	}
	config := restclient.NewConfigBuilder(serverAddress).
		APIPath(defaultTurboAPIPath).
		BasicAuthentication(commConfig.OpsManagerUsername, commConfig.OpsManagerPassword).
		Create()
	glog.V(4).Infof("The Turbo API client config authentication is: %s, %s", commConfig.OpsManagerUsername, commConfig.OpsManagerPassword)
	glog.V(4).Infof("The Turbo API client config is create successfully: %v", config)
	apiClient, err := restclient.NewAPIClientWithBA(config)
	if err != nil {
		builder.err = fmt.Errorf("Error during create Turbo API client: %s\n", err)
		return builder
	}
	glog.V(4).Infof("The Turbo API client is create successfully: %v", apiClient)
	builder.tapService.Client = apiClient

	return builder
}

// The TurboProbe representing the service in the Turbo server
func (builder *TAPServiceBuilder) WithTurboProbe(probeBuilder *probe.ProbeBuilder) *TAPServiceBuilder {
	if builder.err != nil {
		return builder
	}
	// Create the probe
	turboProbe, err := probeBuilder.Create()
	if err != nil {
		builder.err = err
		return builder
	}

	builder.tapService.TurboProbe = turboProbe
	// Register the probe
	err = mediationcontainer.LoadProbe(turboProbe)
	if err != nil {
		builder.err = err
		return builder
	}

	return builder
}
