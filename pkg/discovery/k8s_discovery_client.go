package discovery

import (
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	kubeClient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
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
	newDiscoveryResultDTOs, err := dc.DiscoverWithNewFramework()
	if err != nil {
		glog.Errorf("Failed to use the new framework to discover current Kubernetes cluster: %s", err)
	}

	//Discover the Kubernetes topology
	glog.V(2).Infof("Discovering Kubernetes cluster...")

	// must have kubeClient to do ParseNode and ParsePod
	if dc.config.kubeClient == nil {
		// TODO make error dto
		return nil, fmt.Errorf("Kubenetes client is nil, error")
	}

	kubeProbe, err := probe.NewK8sProbe(dc.config.kubeClient, dc.config.probeConfig)
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
		EntityDTO: newDiscoveryResultDTOs,
		//EntityDTO: entityDtos,
	}

	compareDiscoveryResults(entityDtos, newDiscoveryResultDTOs)

	return discoveryResponse, nil
}

// A testing function for invoking discovery process with the new discovery framework.
func (dc *K8sDiscoveryClient) DiscoverWithNewFramework() ([]*proto.EntityDTO, error) {
	workerConfig := worker.NewK8sDiscoveryWorkerConfig(dc.config.kubeClient, dc.config.probeConfig.StitchingPropertyType)
	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.config.kubeClient)
	for _, mc := range dc.config.probeConfig.MonitoringConfigs {
		workerConfig.WithMonitoringWorkerConfig(mc)
	}
	// create workers
	discoveryWorker, err := worker.NewK8sDiscoveryWorker(workerConfig)
	if err != nil {
		fmt.Errorf("failed to build discovery worker %s", err)
	}

	// create tasks
	listOption := metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
		FieldSelector: fields.Everything().String(),
	}
	nodeList, err := dc.config.kubeClient.Nodes().List(listOption)
	var nodes []*api.Node
	for _, n := range nodeList.Items {
		node := n
		nodes = append(nodes, &node)
	}
	task := task.NewTask().WithNodes(nodes)

	// assign task
	discoveryWorker.ReceiveTask(task)

	// TODO make this async
	// execute task
	result := discoveryWorker.Do()

	if result.Err() != nil {
		return nil, fmt.Errorf("failed to discover current Kubernetes cluster with the new discovery framework: %s", result.Err())
	}

	var entityDTOs []*proto.EntityDTO
	entityDTOs = append(entityDTOs, result.Content()...)

	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcDiscWorker.ReceiveTask(task)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		return nil, fmt.Errorf("failed to discover services from current Kubernetes cluster with the new discovery framework: %s", result.Err())
	}
	entityDTOs = append(entityDTOs, svcDiscResult.Content()...)

	return entityDTOs, nil
}
