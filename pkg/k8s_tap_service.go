package kubeturbo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	client "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/discovery"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"

	"github.com/golang/glog"
)

type K8sTAPServiceSpec struct {
	*service.TurboCommunicationConfig `json:"communicationConfig,omitempty"`
	*configs.K8sTargetConfig          `json:"targetConfig,omitempty"`
}

func ParseK8sTAPServiceSpec(configFile, defaultTargetName string) (*K8sTAPServiceSpec, error) {
	// load the config
	tapSpec, err := readK8sTAPServiceSpec(configFile)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("K8sTapSericeSpec is: %+v", tapSpec)

	if tapSpec.TurboCommunicationConfig == nil {
		return nil, errors.New("communication config is missing")
	}
	if err := tapSpec.ValidateTurboCommunicationConfig(); err != nil {
		return nil, err
	}

	if tapSpec.K8sTargetConfig == nil {
		if defaultTargetName == "" {
			return nil, errors.New("target name is empty")
		}
		tapSpec.K8sTargetConfig = &configs.K8sTargetConfig{TargetIdentifier: defaultTargetName}
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

func createTargetConfig(kubeConfig *restclient.Config) *configs.K8sTargetConfig {
	return &configs.K8sTargetConfig{TargetIdentifier: kubeConfig.Host}
}

type K8sTAPServiceConfig struct {
	spec                     *K8sTAPServiceSpec
	probeConfig              *configs.ProbeConfig
	registrationClientConfig *registration.RegistrationConfig
	discoveryClientConfig    *discovery.DiscoveryClientConfig
}

func NewK8sTAPServiceConfig(kubeClient *client.Clientset, probeConfig *configs.ProbeConfig,
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
