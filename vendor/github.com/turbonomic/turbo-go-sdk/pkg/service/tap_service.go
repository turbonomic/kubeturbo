package service

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/turbo-api/pkg/api"
	"github.com/turbonomic/turbo-api/pkg/client"
	"github.com/turbonomic/turbo-go-sdk/pkg/mediationcontainer"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"net/url"
	"time"
)

// Turbonomic Automation Service that will discover the environment for the Turbo server
// and receive recommendations to control the infrastructure environment
type TAPService struct {
	// TurboProbe provides the communication interface between the Turbo server
	// and the infrastructure environment
	*probe.TurboProbe
	turboClient                 *client.TurboClient
	disconnectFromTurbo         chan struct{}
	communicationBindingChannel string
}

func (tapService *TAPService) DisconnectFromTurbo() {
	glog.V(4).Infof("[DisconnectFromTurbo] Enter *******")
	close(tapService.disconnectFromTurbo)
	glog.V(4).Infof("[DisconnectFromTurbo] End *********")
}

// get the jwtToken from the auth component using client id and client secret fed from k8s secret
// client id and secret are not provided, empty jwtToken will be sent to the channel for websocket connection
// empty jwtToken will be ignored in websocket
func (tapService *TAPService) getJwtToken(refreshTokenChannel chan struct{}, jwTokenChannel chan string) {
	for {
		// blocked by isActive
		_, ok := <-refreshTokenChannel
		if !ok {
			return
		}
		hydraToken := ""
		jwtToken := ""
		var err error
		for {
			hydraToken, err = tapService.turboClient.GetHydraAccessToken()
			if err != nil {
				glog.Errorf("Failed to get hydra token due to %v, retry in 30s", err)
				time.Sleep(time.Second * 30)
			} else {
				break
			}
		}
		for {
			jwtToken, err = tapService.turboClient.GetJwtToken(hydraToken)
			if err != nil {
				glog.Errorf("Failed to get jwt token due to %v, retry in 30s", err)
				time.Sleep(time.Second * 30)
			} else {
				break
			}
		}
		// send jwt Token back
		jwTokenChannel <- jwtToken
	}
}

func (tapService *TAPService) addTarget(isRegistered chan bool) {
	targetInfos := tapService.GetProbeTargets()
	if len(targetInfos) <= 0 {
		return
	}
	// Block until probe is registered
	status := <-isRegistered
	if !status {
		c := tapService.ProbeConfiguration
		pInfo := c.ProbeCategory + "::" + c.ProbeType
		glog.Errorf("Probe %v registration failed.", pInfo)
		return
	}

	for _, targetInfo := range targetInfos {
		target := targetInfo.GetTargetInstance()
		target.InputFields = append(target.InputFields, &api.InputField{
			Name: api.CommunicationBindingChannel, Value: tapService.communicationBindingChannel})
		service := mediationcontainer.GetMediationService()
		if err := tapService.turboClient.AddTarget(target, service); err != nil {
			glog.Errorf("Failed to add target %v: %v",
				targetInfo, err)
		}
	}
}

func (tapService *TAPService) ConnectToTurbo() {
	if tapService.turboClient == nil {
		glog.V(1).Infof("TAP service cannot be started - cannot create target")
		return
	}

	tapService.disconnectFromTurbo = make(chan struct{})
	isRegistered := make(chan bool, 1)
	defer close(isRegistered)
	shouldRefresh := make(chan struct{})
	tokenChannel := make(chan string)
	defer close(shouldRefresh)
	defer close(tokenChannel)

	// start a separate go routine to connect to the Turbo server
	go mediationcontainer.InitMediationContainer(isRegistered, tapService.disconnectFromTurbo, shouldRefresh, tokenChannel)
	go tapService.getJwtToken(shouldRefresh, tokenChannel)
	go tapService.addTarget(isRegistered)
	// Once connected the mediation container will keep running till a disconnect message is sent to the tap service
	select {
	case <-tapService.disconnectFromTurbo:
		glog.V(1).Infof("Begin to stop TAP service.")
		mediationcontainer.CloseMediationContainer()
		glog.V(1).Infof("TAP service is stopped.")
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

// WithCommunicationBindingChannel records the given binding channel in the builder
func (builder *TAPServiceBuilder) WithCommunicationBindingChannel(communicationBindingChannel string) *TAPServiceBuilder {
	builder.tapService.communicationBindingChannel = communicationBindingChannel
	return builder
}

// Build the mediation container and Turbo API client.
func (builder *TAPServiceBuilder) WithTurboCommunicator(commConfig *TurboCommunicationConfig) *TAPServiceBuilder {
	if builder.err != nil {
		return builder
	}

	// Create the mediation container. This is a singleton.
	containerConfig := &mediationcontainer.MediationContainerConfig{
		ServerMeta:                  commConfig.ServerMeta,
		WebSocketConfig:             commConfig.WebSocketConfig,
		CommunicationBindingChannel: builder.tapService.communicationBindingChannel,
	}
	mediationcontainer.CreateMediationContainer(containerConfig)

	// The RestAPI Handler
	serverAddress, err := url.Parse(commConfig.TurboServer)
	if err != nil {
		builder.err = fmt.Errorf("Error during create Turbo API client config: Incorrect URL: %s\n", err)
		return builder
	}
	config := client.NewConfigBuilder(serverAddress).
		BasicAuthentication(url.QueryEscape(commConfig.OpsManagerUsername), url.QueryEscape(commConfig.OpsManagerPassword)).
		SetProxy(commConfig.ServerMeta.Proxy).
		SetClientId(commConfig.ServerMeta.ClientId).SetClientSecret(commConfig.ServerMeta.ClientSecret).
		Create()
	glog.V(4).Infof("The Turbo API client config is create successfully: %v", config)
	builder.tapService.turboClient, err = client.NewTurboClient(config)
	if err != nil {
		builder.err = fmt.Errorf("failed to create Turbo API client: %v", err)
		return builder
	}
	glog.V(4).Infof("The Turbo API client is created successfully: %v",
		builder.tapService.turboClient)
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
