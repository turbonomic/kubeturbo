package kubeturbo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang/glog"
	"io/ioutil"

	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery"
	k8sprobe "github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"
)

type K8sTAPServiceSpec struct {
	*service.TurboCommunicationConfig `json:"communicationConfig,omitempty"`
	*discovery.K8sTargetConfig        `json:"targetConfig,omitempty"`
}

func ParseK8sTAPServiceSpec(configFile string) (*K8sTAPServiceSpec, error) {
	// load the config
	tapSpec, err := readK8sTAPServiceSpec(configFile)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("K8sTapSericeSpec is: %v", tapSpec)

	if tapSpec.TurboCommunicationConfig == nil {
		return nil, errors.New("Communication config is missing")
	}
	if err := tapSpec.ValidateTurboCommunicationConfig(); err != nil {
		return nil, err
	}

	if tapSpec.K8sTargetConfig == nil {
		return nil, errors.New("Target config is missing")
	}
	if err := tapSpec.ValidateK8sTargetConfig(); err != nil {
		return nil, err
	}
	return tapSpec, nil
}

func readK8sTAPServiceSpec(path string) (*K8sTAPServiceSpec, error) {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		return nil, fmt.Errorf("File error: %v\n" + e.Error())
	}
	var spec K8sTAPServiceSpec
	err := json.Unmarshal(file, &spec)
	if err != nil {
		return nil, fmt.Errorf("Unmarshall error :%v", err.Error())
	}
	return &spec, nil
}

type K8sTAPServiceConfig struct {
	spec                     *K8sTAPServiceSpec
	probeConfig              *k8sprobe.ProbeConfig
	registrationClientConfig *registration.RegistrationConfig
	discoveryClientConfig    *discovery.DiscoveryConfig
}

func NewK8sTAPServiceConfig(kubeClient *client.Clientset, probeConfig *k8sprobe.ProbeConfig,
	spec *K8sTAPServiceSpec) *K8sTAPServiceConfig {
	registrationClientConfig := registration.NewRegistrationClientConfig(probeConfig.StitchingPropertyType)
	discoveryClientConfig := discovery.NewDiscoveryConfig(kubeClient, probeConfig, spec.K8sTargetConfig)
	return &K8sTAPServiceConfig{
		spec: spec,
		registrationClientConfig: registrationClientConfig,
		discoveryClientConfig:    discoveryClientConfig,
	}
}

type K8sTAPService struct {
	*service.TAPService
}

func NewKubernetesTAPService(config *K8sTAPServiceConfig, actionHandler *action.ActionHandler) (*K8sTAPService, error) {
	if config == nil || config.spec == nil {
		return nil, errors.New("Invalid K8sTAPServiceConfig")
	}
	// Kubernetes Probe Registration Client
	registrationClient := registration.NewK8sRegistrationClient(config.registrationClientConfig)
	// Kubernetes Probe Discovery Client
	discoveryClient := discovery.NewK8sDiscoveryClient(config.discoveryClientConfig)

	tapService, err :=
		service.NewTAPServiceBuilder().
			WithTurboCommunicator(config.spec.TurboCommunicationConfig).
			WithTurboProbe(probe.NewProbeBuilder(config.spec.TargetType, config.spec.ProbeCategory).
				RegisteredBy(registrationClient).
				DiscoversTarget(config.spec.TargetIdentifier, discoveryClient).
				ExecutesActionsBy(actionHandler)).
			Create()
	if err != nil {
		return nil, fmt.Errorf("Error when creating KubernetesTAPService: %s", err)
	}

	return &K8sTAPService{tapService}, nil
}

func (s *K8sTAPService) Run() {
	s.ConnectToTurbo()
}
