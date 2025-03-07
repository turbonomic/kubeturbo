package mediationcontainer

import (
	protobuf "github.com/golang/protobuf/proto"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/version"

	"fmt"
	"time"

	"github.com/golang/glog"
)

type SdkClientProtocol struct {
	allProbes                    map[string]*ProbeProperties
	version                      string
	communicationBindingChannel  string
	oauth2ClientId               string
	registrationResponseTimeout  time.Duration
	restartOnRegistrationTimeout bool
}

func CreateSdkClientProtocolHandler(allProbes map[string]*ProbeProperties, version, communicationBindingChannel, oauth2ClientId string,
	sdkProtocolConfig *SdkProtocolConfig) *SdkClientProtocol {

	registrationResponseTimeout := time.Second * DefaultRegistrationTimeOut
	restartOnRegistrationTimeout := true

	if sdkProtocolConfig != nil {
		if sdkProtocolConfig.RegistrationTimeoutSec >= DefaultRegistrationTimeoutThreshold {
			var timeout time.Duration
			timeout = time.Duration(sdkProtocolConfig.RegistrationTimeoutSec)
			registrationResponseTimeout = time.Second * timeout
		}

		restartOnRegistrationTimeout = sdkProtocolConfig.RestartOnRegistrationTimeout
	}
	glog.Infof("SDK Protocol timeout related config [%+v]", sdkProtocolConfig)

	return &SdkClientProtocol{
		allProbes:                    allProbes,
		version:                      version,
		communicationBindingChannel:  communicationBindingChannel,
		oauth2ClientId:               oauth2ClientId,
		registrationResponseTimeout:  registrationResponseTimeout,
		restartOnRegistrationTimeout: restartOnRegistrationTimeout,
	}
}

func (clientProtocol *SdkClientProtocol) handleClientProtocol(transport ITransport, transportReady chan bool) {
	glog.V(2).Infof("Starting protocol negotiation ....")
	status := clientProtocol.NegotiateVersion(transport)

	if !status {
		glog.Errorf("Failure during Protocol Negotiation, Registration message will not be sent")
		transportReady <- false
		// clientProtocol.TransportReady <- false
		return
	}
	glog.V(2).Infof("Starting probe registration ....")
	status = clientProtocol.HandleRegistration(transport)
	if !status {
		glog.Errorf("Failure during Registration, cannot receive server messages")
		// exit here ... so Kubernetes can restart the probe container
		if clientProtocol.restartOnRegistrationTimeout {
			panic("********* Failure during Registration **********")
		}
		transportReady <- false
		// clientProtocol.TransportReady <- false
		return
	}

	transportReady <- true
	// clientProtocol.TransportReady <- true
}

// ============================== Protocol Version Negotiation =========================
func timeOutRead(name string, du time.Duration, ch chan *ParsedMessage) (*ParsedMessage, error) {
	timer := time.NewTimer(du)
	select {
	case msg, ok := <-ch:
		if !ok {
			err := fmt.Errorf("[%s]: Endpoint Receiver channel is closed", name)
			glog.Error(err.Error())
			return nil, err
		}
		if msg == nil {
			err := fmt.Errorf("[%s]: Endpoint receive null message", name)
			glog.Error(err.Error())
			return nil, err
		}
		return msg, nil
	case <-timer.C:
		glog.Infof("[%s]: timeout during version negotiation/registration after %v seconds", name, du.Seconds())
		err := fmt.Errorf("[%s]: wait for message from channel timeout(%v seconds)", name, du.Seconds())
		glog.Error(err.Error())
		return nil, err
	}
}

func (clientProtocol *SdkClientProtocol) NegotiateVersion(transport ITransport) bool {
	versionStr := clientProtocol.version
	request := &version.NegotiationRequest{
		ProtocolVersion: &versionStr,
	}
	glog.V(4).Infof("Send negotiation message: %+v", request)

	// Create Protobuf Endpoint to send and handle negotiation messages
	protoMsg := &NegotiationResponse{} // handler for the response
	endpoint := CreateClientProtoBufEndpoint("NegotiationEndpoint", transport, protoMsg, true)
	defer endpoint.CloseEndpoint()

	endMsg := &EndpointMessage{
		ProtobufMessage: request,
	}
	endpoint.Send(endMsg)

	// Wait for the response to be received by the transport and then parsed and put on the endpoint's message channel
	negotiationStartTime := time.Now()
	serverMsg, err := timeOutRead(endpoint.GetName(), clientProtocol.registrationResponseTimeout, endpoint.MessageReceiver())
	if err != nil {
		glog.V(2).Infof("[%s] : Error during version negotiation: %+v, disconnecting from server", endpoint.GetName(), err)
		glog.Errorf("[%s] : read VersionNegotiation response from channel failed: %v", endpoint.GetName(), err)
		return false
	}
	negotiationEndTime := time.Now()
	negotiationTime := negotiationEndTime.Sub(negotiationStartTime)

	glog.V(2).Infof("[%s] : Received VersionNegotiation response after %v ",
		endpoint.GetName(), negotiationTime)

	glog.V(4).Infof("[%s] : Received: %+v", endpoint.GetName(), serverMsg)

	// Handler response
	negotiationResponse := protoMsg.NegotiationMsg
	if negotiationResponse == nil {
		glog.Error("Probe Protocol failed, null negotiation response")
		return false
	}

	if negotiationResponse.GetNegotiationResult().String() != version.NegotiationAnswer_ACCEPTED.String() {
		glog.Errorf("Protocol version negotiation failed %s",
			negotiationResponse.GetNegotiationResult().String()+") :"+negotiationResponse.GetDescription())
		return false
	}

	glog.V(2).Infof("Protocol negotiation result: %+v. %s.",
		serverMsg.NegotiationMsg.GetNegotiationResult(), serverMsg.NegotiationMsg.GetDescription())
	return true
}

// ======================= Registration ============================
// Send registration message
func (clientProtocol *SdkClientProtocol) HandleRegistration(transport ITransport) bool {
	containerInfo, err := clientProtocol.MakeContainerInfo()
	if err != nil {
		glog.Error("Error creating ContainerInfo")
		return false
	}

	glog.V(3).Infof("containerInfo: %s", protobuf.MarshalTextString(containerInfo))

	// Create Protobuf Endpoint to send and handle registration messages
	protoMsg := &RegistrationResponse{}
	endpoint := CreateClientProtoBufEndpoint("RegistrationEndpoint", transport, protoMsg, true)
	defer endpoint.CloseEndpoint()

	endMsg := &EndpointMessage{
		ProtobufMessage: containerInfo,
	}
	endpoint.Send(endMsg)

	// Wait for the response to be received by the transport and then parsed and put on the endpoint's message channel

	registrationStartTime := time.Now()
	serverMsg, err := timeOutRead(endpoint.GetName(), clientProtocol.registrationResponseTimeout, endpoint.MessageReceiver())
	if err != nil {
		glog.V(2).Infof("[%s] : Error during registration: %+v, disconnecting from server", endpoint.GetName(), err)
		glog.Errorf("[%s] : read Registration response from channel failed: %v", endpoint.GetName(), err)
		return false
	}
	registrationEndTime := time.Now()
	registrationTime := registrationEndTime.Sub(registrationStartTime)

	glog.V(2).Infof("[%s] : Received registration response after %v",
		endpoint.GetName(), registrationTime)

	glog.V(4).Infof("[%s] : Received registration response: %+v", endpoint.GetName(), serverMsg)

	// Handler response
	registrationResponse := protoMsg.RegistrationMsg
	if registrationResponse == nil {
		glog.Errorf("Probe registration failed, null ack.")
		return false
	}
	glog.V(2).Infof("Probe registration succeeded.")
	return true
}

func (clientProtocol *SdkClientProtocol) MakeContainerInfo() (*proto.ContainerInfo, error) {
	var probes []*proto.ProbeInfo

	for k, v := range clientProtocol.allProbes {
		glog.V(2).Infof("SdkClientProtocol] Creating Probe Info for %s", k)
		turboProbe := v.Probe
		var probeInfo *proto.ProbeInfo
		var err error
		probeInfo, err = turboProbe.GetProbeInfo()

		if err != nil {
			return nil, err
		}
		probes = append(probes, probeInfo)
	}

	return &proto.ContainerInfo{
		Probes:                      probes,
		CommunicationBindingChannel: &clientProtocol.communicationBindingChannel,
		Oauth2ClientId:              &clientProtocol.oauth2ClientId,
	}, nil
}
