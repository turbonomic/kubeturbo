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
	"github.com/turbonomic/kubeturbo/pkg/registration"
	"github.com/turbonomic/kubeturbo/version"
	"github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/service"
)

const (
	defaultUsername  = "defaultUser"
	defaultPassword  = "defaultPassword"
	usernameFilePath = "/etc/turbonomic-credentials/username"
	passwordFilePath = "/etc/turbonomic-credentials/password"
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

	if err := loadOpsMgrCredentialsFromSecret(tapSpec); err != nil {
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

func createProbeConfigOrDie(c *Config) *configs.ProbeConfig {
	// Create Kubelet monitoring
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(c.KubeletClient, c.KubeClient)

	// Create cluster monitoring
	clusterScraper := cluster.NewClusterScraper(c.KubeClient, c.DynamicClient, c.clusterAPIEnabled, c.CAClient, c.CAPINamespace)
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
		config.DiscoverySamples, config.DiscoverySampleIntervalSec)

	actionHandlerConfig := action.NewActionHandlerConfig(config.CAPINamespace, config.CAClient, config.KubeletClient,
		probeConfig.ClusterScraper, config.SccSupport, config.ORMClient, config.failVolumePodMoves,
		config.updateQuotaToAllowMoves, config.readinessRetryThreshold, config.gitConfig)

	// Kubernetes Probe Registration Client
	registrationClient := registration.NewK8sRegistrationClient(registrationClientConfig, config.tapSpec.K8sTargetConfig)

	// Kubernetes Probe Discovery Client
	discoveryClient := discovery.NewK8sDiscoveryClient(discoveryClientConfig)

	// Kubernetes Probe Action Execution Client
	actionHandler := action.NewActionHandler(actionHandlerConfig)

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
		// The KubeTurbo TAP Service that will register the kubernetes target with the
		// Turbonomic server and await for validation, discovery, action execution requests
		glog.Infof("Should discover target %s", config.tapSpec.TargetIdentifier)
		probeBuilder = probeBuilder.DiscoversTarget(config.tapSpec.TargetIdentifier, discoveryClient)
	} else {
		glog.Infof("Not discovering target")
		probeBuilder = probeBuilder.WithDiscoveryClient(discoveryClient)
	}

	k8sSvcId, err := probeConfig.ClusterScraper.GetKubernetesServiceID()
	if err != nil {
		glog.Fatalf("Error retrieving the Kubernetes service id: %v", err)
	}
	tapService, err :=
		service.NewTAPServiceBuilder().
			WithCommunicationBindingChannel(k8sSvcId).
			WithTurboCommunicator(config.tapSpec.TurboCommunicationConfig).
			WithTurboProbe(probeBuilder).
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
