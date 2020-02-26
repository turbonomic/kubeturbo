package kubeturbo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"

	restclient "k8s.io/client-go/rest"

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/discovery"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
)

const (
	usernameFilePath = "/etc/turbonomic-credentials/username"
	passwordFilePath = "/etc/turbonomic-credentials/password"
)

type K8sTAPServiceSpec struct {
	*service.TurboCommunicationConfig `json:"communicationConfig,omitempty"`
	*configs.K8sTargetConfig          `json:"targetConfig,omitempty"`
	*detectors.MasterNodeDetectors    `json:"masterNodeDetectors,omitempty"`
	*detectors.DaemonPodDetectors     `json:"daemonPodDetectors,omitempty"`
	*detectors.HANodeConfig           `json:"HANodeConfig,omitempty"`
}

func ParseK8sTAPServiceSpec(configFile, defaultTargetName string) (*K8sTAPServiceSpec, error) {
	// load the config
	tapSpec, err := readK8sTAPServiceSpec(configFile)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("K8sTapServiceSpec is: %+v", tapSpec)

	if tapSpec.TurboCommunicationConfig == nil {
		return nil, errors.New("communication config is missing")
	}

	if err := loadOpsMgrCredentialsFromSecret(tapSpec); err != nil {
		return nil, err
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

	// This function aborts the program upon fatal error
	detectors.ValidateAndParseDetectors(tapSpec.MasterNodeDetectors,
		tapSpec.DaemonPodDetectors, tapSpec.HANodeConfig)

	return tapSpec, nil
}

func loadOpsMgrCredentialsFromSecret(tapSpec *K8sTAPServiceSpec) error {
	// Return unchanged if the mounted file isn't present
	// for backward compatibility.
	if _, err := os.Stat(usernameFilePath); os.IsNotExist(err) {
		glog.V(3).Infof("credentials from secret unavailable. Checked path: %s", usernameFilePath)
		return nil
	}
	if _, err := os.Stat(passwordFilePath); os.IsNotExist(err) {
		glog.V(3).Infof("credentials from secret unavailable. Checked path: %s", passwordFilePath)
		return nil
	}

	username, err := ioutil.ReadFile(usernameFilePath)
	if err != nil {
		return fmt.Errorf("error reading credentials from secret: username: %v", err)
	}
	password, err := ioutil.ReadFile(passwordFilePath)
	if err != nil {
		return fmt.Errorf("error reading credentials from secret: password: %v", err)
	}

	tapSpec.OpsManagerUsername = strings.TrimSpace(string(username))
	tapSpec.OpsManagerPassword = strings.TrimSpace(string(password))

	return nil
}

func readK8sTAPServiceSpec(path string) (*K8sTAPServiceSpec, error) {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		return nil, fmt.Errorf("file error: %v" + e.Error())
	}
	var spec K8sTAPServiceSpec
	err := json.Unmarshal(file, &spec)
	if err != nil {
		return nil, fmt.Errorf("unmarshall error :%v", err.Error())
	}
	return &spec, nil
}

func createTargetConfig(kubeConfig *restclient.Config) *configs.K8sTargetConfig {
	return &configs.K8sTargetConfig{TargetIdentifier: kubeConfig.Host}
}

func createProbeConfigOrDie(c *Config) *configs.ProbeConfig {
	// Create Kubelet monitoring
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(c.KubeletClient)

	// Create cluster monitoring
	masterMonitoringConfig := master.NewClusterMonitorConfig(c.KubeClient, c.DynamicClient)

	// TODO for now kubelet is the only monitoring source. As we have more sources, we should choose what to be added into the slice here.
	monitoringConfigs := []monitoring.MonitorWorkerConfig{
		kubeletMonitoringConfig,
		masterMonitoringConfig,
	}

	probeConfig := &configs.ProbeConfig{
		StitchingPropertyType: c.StitchingPropType,
		MonitoringConfigs:     monitoringConfigs,
		ClusterClient:         c.KubeClient,
		DynamicClient:         c.DynamicClient,
		NodeClient:            c.KubeletClient,
	}

	return probeConfig
}

type K8sTAPService struct {
	*service.TAPService
}

func NewKubernetesTAPService(config *Config) (*K8sTAPService, error) {
	if config == nil || config.tapSpec == nil {
		return nil, errors.New("invalid K8sTAPServiceConfig")
	}

	// Create the configurations for the registration, discovery and action clients
	registrationClientConfig := registration.NewRegistrationClientConfig(config.StitchingPropType, config.VMPriority, config.VMIsBase)

	probeConfig := createProbeConfigOrDie(config)
	discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, config.tapSpec.K8sTargetConfig, config.ValidationWorkers, config.ValidationTimeoutSec)

	actionHandlerConfig := action.NewActionHandlerConfig(config.CAPINamespace, config.CAClient, config.KubeClient, config.KubeletClient, config.DynamicClient, config.SccSupport)

	// Kubernetes Probe Registration Client
	registrationClient := registration.NewK8sRegistrationClient(registrationClientConfig)

	// Kubernetes Probe Discovery Client
	discoveryClient := discovery.NewK8sDiscoveryClient(discoveryClientConfig)

	// Kubernetes Probe Action Execution Client
	actionHandler := action.NewActionHandler(actionHandlerConfig)

	// The KubeTurbo TAP Service that will register the kubernetes target with the
	// Turbonomic server and await for validation, discovery, action execution requests
	tapService, err :=
		service.NewTAPServiceBuilder().
			WithTurboCommunicator(config.tapSpec.TurboCommunicationConfig).
			WithTurboProbe(probe.NewProbeBuilder(config.tapSpec.TargetType, config.tapSpec.ProbeCategory).
				WithDiscoveryOptions(probe.FullRediscoveryIntervalSecondsOption(int32(config.DiscoveryIntervalSec))).
				RegisteredBy(registrationClient).
				WithActionPolicies(registrationClient).
				WithEntityMetadata(registrationClient).
				DiscoversTarget(config.tapSpec.TargetIdentifier, discoveryClient).
				ExecutesActionsBy(actionHandler)).
			Create()
	if err != nil {
		return nil, err
	}

	return &K8sTAPService{tapService}, nil
}

func (s *K8sTAPService) Run() {
	s.ConnectToTurbo()
}
