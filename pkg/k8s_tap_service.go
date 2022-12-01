package kubeturbo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/registration"
	"github.com/turbonomic/kubeturbo/version"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"
)

const (
	defaultUsername      = "defaultUser"
	defaultPassword      = "defaultPassword"
	credentialsDirPath   = "/etc/turbonomic-credentials"
	usernameFilePath     = "/etc/turbonomic-credentials/username"
	passwordFilePath     = "/etc/turbonomic-credentials/password"
	clientIdFilePath     = "/etc/turbonomic-credentials/clientid"
	clientSecretFilePath = "/etc/turbonomic-credentials/clientsecret"
)

type K8sTAPServiceSpec struct {
	*service.TurboCommunicationConfig `json:"communicationConfig,omitempty"`
	*configs.K8sTargetConfig          `json:"targetConfig,omitempty"`
	*detectors.MasterNodeDetectors    `json:"masterNodeDetectors,omitempty"`
	*detectors.DaemonPodDetectors     `json:"daemonPodDetectors,omitempty"`
	*detectors.HANodeConfig           `json:"HANodeConfig,omitempty"`
	*detectors.AnnotationWhitelist    `json:"annotationWhitelist,omitempty"`
	FeatureGates                      map[string]bool `json:"featureGates,omitempty"`
}

func ParseK8sTAPServiceSpec(configFile string, defaultTargetName string) (*K8sTAPServiceSpec, error) {
	// load the config
	tapSpec, err := readK8sTAPServiceSpec(configFile)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("K8sTapServiceSpec is: %+v", tapSpec)

	if tapSpec.TurboCommunicationConfig == nil {
		return nil, errors.New("communication config is missing")
	}

	if tapSpec.K8sTargetConfig == nil {
		// The targetConfig is missing, create one
		tapSpec.K8sTargetConfig = &configs.K8sTargetConfig{}
	}

	if tapSpec.TargetIdentifier == "" && tapSpec.TargetType == "" {
		// Neither targetIdentifier nor targetType is specified, set a default target name
		if defaultTargetName == "" {
			return nil, errors.New("default target name is empty")
		}
		tapSpec.TargetIdentifier = defaultTargetName
	}

	if _, err := os.Stat(credentialsDirPath); os.IsNotExist(err) {
		glog.V(2).Infof("credentials mount path %s does not exist", credentialsDirPath)
	}

	if err := loadOpsMgrCredentialsFromSecret(tapSpec); err != nil {
		return nil, err
	}

	if err := loadClientIdSecretFromSecret(tapSpec); err != nil {
		return nil, err
	}
	if err := tapSpec.ValidateTurboCommunicationConfig(); err != nil {
		return nil, err
	}

	if err := tapSpec.ValidateK8sTargetConfig(); err != nil {
		return nil, err
	}

	// This function aborts the program upon fatal error
	detectors.ValidateAndParseDetectors(tapSpec.MasterNodeDetectors,
		tapSpec.DaemonPodDetectors, tapSpec.HANodeConfig, tapSpec.AnnotationWhitelist)

	logFeatureGates(tapSpec)

	return tapSpec, nil
}

func logFeatureGates(tapSpec *K8sTAPServiceSpec) {
	featureGates := make(map[string]bool)
	for featureGate, flag := range tapSpec.FeatureGates {
		featureGates[featureGate] = flag
	}
	for feature, spec := range features.DefaultKubeturboFeatureGates {
		if _, exists := featureGates[string(feature)]; !exists {
			featureGates[string(feature)] = spec.Default
		}
	}
	for feature, flag := range featureGates {
		glog.V(2).Infof("FEATURE GATE: %s=%t", feature, flag)
	}
}

func loadOpsMgrCredentialsFromSecret(tapSpec *K8sTAPServiceSpec) error {
	// Return unchanged if the mounted file isn't present
	// for backward compatibility.
	if _, err := os.Stat(usernameFilePath); os.IsNotExist(err) {
		glog.V(2).Infof("server api credentials from secret unavailable. Checked path: %s", usernameFilePath)
		return nil
	}
	if _, err := os.Stat(passwordFilePath); os.IsNotExist(err) {
		glog.V(2).Infof("server api credentials from secret unavailable. Checked path: %s", passwordFilePath)
		return nil
	}

	username, err := ioutil.ReadFile(usernameFilePath)
	if err != nil {
		return fmt.Errorf("error reading server api credentials from secret: username: %v", err)
	}
	password, err := ioutil.ReadFile(passwordFilePath)
	if err != nil {
		return fmt.Errorf("error reading server api credentials from secret: password: %v", err)
	}

	tapSpec.OpsManagerUsername = strings.TrimSpace(string(username))
	tapSpec.OpsManagerPassword = strings.TrimSpace(string(password))

	return nil
}

func loadClientIdSecretFromSecret(tapSpec *K8sTAPServiceSpec) error {
	// Return unchanged if the mounted file isn't present
	// for backward compatibility.
	if _, err := os.Stat(clientIdFilePath); os.IsNotExist(err) {
		glog.V(2).Infof("secure server credentials from secret unavailable. Checked path: %s", clientIdFilePath)
		return nil
	}
	if _, err := os.Stat(clientSecretFilePath); os.IsNotExist(err) {
		glog.V(2).Infof("secure server credentials from secret unavailable. Checked path: %s", clientSecretFilePath)
		return nil
	}

	clientId, err := ioutil.ReadFile(clientIdFilePath)
	if err != nil {
		return fmt.Errorf("error reading secure server credentials from secret: clientId: %v", err)
	}
	clientSecret, err := ioutil.ReadFile(clientSecretFilePath)
	if err != nil {
		return fmt.Errorf("error reading secure server credentials from secret: clientSecret: %v", err)
	}

	tapSpec.ClientId = strings.TrimSpace(string(clientId))
	tapSpec.ClientSecret = strings.TrimSpace(string(clientSecret))
	glog.V(4).Infof("Obtained credentials to set up secure probe communication")
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
		switch typeErr := err.(type) {
		case *json.UnmarshalTypeError:
			return nil, fmt.Errorf("error processing configuration property '%v':%v", typeErr.Field, typeErr.Error())
		default:
			return nil, fmt.Errorf("error processing configuration: %v", err.Error())
		}
	}
	return &spec, nil
}

func createProbeConfigOrDie(c *Config) *configs.ProbeConfig {
	// Create Kubelet monitoring
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(c.KubeletClient, c.KubeClient)

	// Create cluster monitoring
	clusterScraper := cluster.NewClusterScraper(
		c.KubeClient, c.DynamicClient, c.ControllerRuntimeClient, c.clusterAPIEnabled, c.CAClient, c.CAPINamespace)
	masterMonitoringConfig := master.NewClusterMonitorConfig(clusterScraper)

	// TODO for now kubelet is the only monitoring source. As we have more sources, we should choose what to be added into the slice here.
	monitoringConfigs := []monitoring.MonitorWorkerConfig{
		kubeletMonitoringConfig,
		masterMonitoringConfig,
	}

	probeConfig := &configs.ProbeConfig{
		StitchingPropertyType: c.StitchingPropType,
		MonitoringConfigs:     monitoringConfigs,
		ClusterScraper:        clusterScraper,
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
	registrationClientConfig := registration.NewRegistrationClientConfig(config.StitchingPropType, config.VMPriority,
		config.VMIsBase, config.clusterAPIEnabled)

	probeConfig := createProbeConfigOrDie(config)
	discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, config.tapSpec.K8sTargetConfig,
		config.ValidationWorkers, config.ValidationTimeoutSec, config.containerUtilizationDataAggStrategy,
		config.containerUsageDataAggStrategy, config.ORMClient, config.DiscoveryWorkers, config.DiscoveryTimeoutSec,
		config.DiscoverySamples, config.DiscoverySampleIntervalSec, config.ItemsPerListQuery)

	if config.clusterKeyInjected != "" {
		discoveryClientConfig = discoveryClientConfig.WithClusterKeyInjected(config.clusterKeyInjected)
	}

	k8sSvcId, err := probeConfig.ClusterScraper.GetKubernetesServiceID()
	if err != nil {
		glog.Fatalf("Error retrieving the Kubernetes service id: %v", err)
	}
	actionHandlerConfig := action.NewActionHandlerConfig(config.CAPINamespace, config.CAClient, config.KubeletClient,
		probeConfig.ClusterScraper, config.SccSupport, config.ORMClient, config.failVolumePodMoves,
		config.updateQuotaToAllowMoves, config.readinessRetryThreshold, config.gitConfig, k8sSvcId)

	// Kubernetes Probe Discovery Client
	discoveryClient := discovery.NewK8sDiscoveryClient(discoveryClientConfig)
	targetAccountValues := discoveryClient.GetAccountValues()

	// Kubernetes Probe Action Execution Client
	actionHandler := action.NewActionHandler(actionHandlerConfig)

	// Kubernetes Probe Registration Client
	registrationClient := registration.NewK8sRegistrationClient(registrationClientConfig,
		config.tapSpec.K8sTargetConfig, targetAccountValues.AccountValues(), k8sSvcId)

	probeVersion := version.Version
	probeDisplayName := getProbeDisplayName(config.tapSpec.TargetType, config.tapSpec.TargetIdentifier)

	probeBuilder := probe.NewProbeBuilder(config.tapSpec.TargetType,
		config.tapSpec.ProbeCategory, config.tapSpec.ProbeUICategory).
		WithVersion(probeVersion).
		WithDisplayName(probeDisplayName).
		WithDiscoveryOptions(probe.FullRediscoveryIntervalSecondsOption(int32(config.DiscoveryIntervalSec))).
		RegisteredBy(registrationClient).
		WithActionPolicies(registrationClient).
		WithEntityMetadata(registrationClient).
		WithActionMergePolicies(registrationClient).
		ExecutesActionsBy(actionHandler)

	if len(config.tapSpec.TargetIdentifier) > 0 {
		// Target will be added as part of probe registration only when communicating with the server
		// on a secure websocket, else secret containing Turbo server admin user must be configured
		// to auto-add the target using API
		probeBuilder.WithSecureTargetProvider(registrationClient)

		// The KubeTurbo TAP Service that will register the kubernetes target with the
		// Turbonomic server and await for validation, discovery, action execution requests
		glog.Infof("Discovering target %s", config.tapSpec.TargetIdentifier)
		probeBuilder = probeBuilder.DiscoversTarget(config.tapSpec.TargetIdentifier, discoveryClient)
	} else {
		// Target is NOT auto-added if TargetIdentifier is not configured.
		// In this case, users can still add target via the UI.
		// To ensure that the target added via the UI can communication with the server, it is necessary to configure
		// 'TargetType' to uniquely identify the kubernetes cluster this probe is going to monitor.
		// Not configuring 'TargetType' is error-prone and not lead to correct discovery results.
		if len(config.tapSpec.TargetType) > 0 {
			glog.Infof("Not discovering target, add target via API or UI for target type %s", config.tapSpec.TargetType)
		} else {
			glog.Infof("Not discovering target, target type is not configured and discovery may not work correctly")
		}
		probeBuilder = probeBuilder.WithDiscoveryClient(discoveryClient)
	}

	tapService, err :=
		service.NewTAPServiceBuilder().
			WithCommunicationBindingChannel(k8sSvcId).
			WithTurboCommunicator(config.tapSpec.TurboCommunicationConfig).
			WithTurboProbe(probeBuilder). //Turbo Probe is Probe + Target
			Create()
	if err != nil {
		return nil, err
	}

	return &K8sTAPService{tapService}, nil
}

// getProbeDisplayName constructs a display name for the probe based on the input probe type and target id
func getProbeDisplayName(probeType, targetId string) string {
	return strings.Join([]string{probeType, "Probe", targetId}, " ")
}

func (s *K8sTAPService) Run() {
	s.ConnectToTurbo()
}
