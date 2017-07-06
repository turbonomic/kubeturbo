package discovery

import (
	"fmt"
	"sync"
	"time"

	kubeClient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	// TODO make this number programmatically.
	workerCount int = 4
)

// TODO maybe use a discovery client config
type DiscoveryConfig struct {
	k8sClusterScraper *cluster.ClusterScraper

	probeConfig *probe.ProbeConfig

	targetConfig *K8sTargetConfig

	// TODO remove this when the old framework is removed.
	kubeClient *kubeClient.Clientset
}

func NewDiscoveryConfig(kubeClient *kubeClient.Clientset, probeConfig *probe.ProbeConfig, targetConfig *K8sTargetConfig) *DiscoveryConfig {
	return &DiscoveryConfig{
		k8sClusterScraper: &cluster.ClusterScraper{kubeClient},
		probeConfig:       probeConfig,
		targetConfig:      targetConfig,

		kubeClient: kubeClient,
	}
}

type K8sDiscoveryClient struct {
	config *DiscoveryConfig

	dispatcher      *worker.Dispatcher
	resultCollector *worker.ResultCollector

	wg sync.WaitGroup
}

func NewK8sDiscoveryClient(config *DiscoveryConfig) *K8sDiscoveryClient {
	// make maxWorkerCount of result collector twice the worker count.
	resultCollector := worker.NewResultCollector(workerCount * 2)

	dispatcherConfig := worker.NewDispatcherConfig(config.k8sClusterScraper, config.probeConfig, workerCount)
	dispatcher := worker.NewDispatcher(dispatcherConfig)
	dispatcher.Init(resultCollector)

	dc := &K8sDiscoveryClient{
		config:          config,
		dispatcher:      dispatcher,
		resultCollector: resultCollector,
	}
	return dc
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
	currentTime := time.Now()
	newDiscoveryResultDTOs, err := dc.DiscoverWithNewFramework()
	if err != nil {
		glog.Errorf("Failed to use the new framework to discover current Kubernetes cluster: %s", err)
	}

	discoveryResponse := &proto.DiscoveryResponse{
		EntityDTO: newDiscoveryResultDTOs,
		//EntityDTO: entityDtos,
	}

	newFrameworkDiscTime := time.Now().Sub(currentTime).Nanoseconds()
	glog.Infof("New framework discovery time: %d", newFrameworkDiscTime)

	currentTime = time.Now()
	oldDiscoveryResult, err := dc.discoveryWithOldFramework()
	if err != nil {
		glog.Errorf("Failed to discovery with the old framework: %s", err)
	} else {
		oldFrameworkDiscTime := time.Now().Sub(currentTime).Nanoseconds()
		glog.Infof("Old framework discovery time: %d", oldFrameworkDiscTime)

		compareDiscoveryResults(oldDiscoveryResult, newDiscoveryResultDTOs)
	}
	return discoveryResponse, nil
}

func (dc *K8sDiscoveryClient) discoveryWithOldFramework() ([]*proto.EntityDTO, error) {
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

	return entityDtos, nil
}

// A testing function for invoking discovery process with the new discovery framework.
func (dc *K8sDiscoveryClient) DiscoverWithNewFramework() ([]*proto.EntityDTO, error) {
	nodes, err := dc.config.k8sClusterScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in the cluster: %s", err)
	}

	workerCount := dc.dispatcher.Dispatch(nodes)
	entityDTOs := dc.resultCollector.Collect(workerCount)
	glog.V(3).Infof("Discovery workers have finished discovery work with %d entityDTOs built. Now performing service discovery...", len(entityDTOs))

	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.config.k8sClusterScraper)
	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcTask := task.NewTask().WithNodes(nodes)
	svcDiscWorker.ReceiveTask(svcTask)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		glog.Errorf("failed to discover services from current Kubernetes cluster with the new discovery framework: %s", svcDiscResult.Err())
	} else {
		entityDTOs = append(entityDTOs, svcDiscResult.Content()...)
	}

	return entityDTOs, nil
}
