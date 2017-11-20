package discovery

import (
	"fmt"
	"sync"
	"time"

	kubeClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance"
	"github.com/turbonomic/kubeturbo/pkg/registration"

	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/processor"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const (
	// TODO make this number programmatically.
	workerCount int = 4
)

type DiscoveryClientConfig struct {
	k8sClusterScraper *cluster.ClusterScraper

	probeConfig *configs.ProbeConfig

	targetConfig *configs.K8sTargetConfig
}

func NewDiscoveryConfig(kubeClient *kubeClient.Clientset, probeConfig *configs.ProbeConfig, targetConfig *configs.K8sTargetConfig) *DiscoveryClientConfig {
	return &DiscoveryClientConfig{
		k8sClusterScraper: &cluster.ClusterScraper{kubeClient},
		probeConfig:       probeConfig,
		targetConfig:      targetConfig,
	}
}

// Implements the go sdk discovery client interface
type K8sDiscoveryClient struct {
	config *DiscoveryClientConfig

	dispatcher      *worker.Dispatcher
	resultCollector *worker.ResultCollector

	wg sync.WaitGroup
}

func NewK8sDiscoveryClient(config *DiscoveryClientConfig) *K8sDiscoveryClient {
	// make maxWorkerCount of result collector twice the worker count.
	resultCollector := worker.NewResultCollector(workerCount * 2)

	dispatcherConfig := worker.NewDispatcherConfig(config.k8sClusterScraper, config.probeConfig, workerCount)
	dispatcher := worker.NewDispatcher(dispatcherConfig)
	//TODO: dispatcher.Init(resultCollector) - moved to discoverWithNewFramework() after the cluster discovery is completed

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
	newDiscoveryResultDTOs, err := dc.discoverWithNewFramework()
	if err != nil {
		glog.Errorf("Failed to use the new framework to discover current Kubernetes cluster: %s", err)
	}

	discoveryResponse := &proto.DiscoveryResponse{
		EntityDTO: newDiscoveryResultDTOs,
	}

	newFrameworkDiscTime := time.Now().Sub(currentTime).Seconds()
	glog.V(2).Infof("New framework discovery time: %.3f seconds", newFrameworkDiscTime)

	return discoveryResponse, nil
}

func (dc *K8sDiscoveryClient) discoverWithNewFramework() ([]*proto.EntityDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES AND QUOTAS HERE
	clusterProcessor := &processor.ClusterProcessor{
				ClusterInfoScraper: dc.config.k8sClusterScraper,
				}
	kubeCluster, err := clusterProcessor.ProcessCluster()
	if err != nil {
		return nil, fmt.Errorf("Failed to process cluster: %s", err)
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	// Initialize the Dispatcher to create discovery workers
	dc.dispatcher.Init(dc.resultCollector, clusterSummary)	//need to pass the cluster object to the discovery workers

	nodes := clusterSummary.NodeList
	workerCount := dc.dispatcher.Dispatch(nodes)
	entityDTOs, quotaMetricsList := dc.resultCollector.Collect(workerCount)

	quotasDiscoveryWorker := worker.Newk8sResourceQuotasDiscoveryWorker(clusterSummary)
	quotaDtos, _ := quotasDiscoveryWorker.Do(quotaMetricsList)

	entityDTOs = append(entityDTOs, quotaDtos...)

	glog.V(2).Infof("Discovery workers have finished discovery work with %d entityDTOs built. Now performing service discovery...", len(entityDTOs))

	// affinity process
	glog.V(2).Infof("begin to process affinity.")
	affinityProcessorConfig := compliance.NewAffinityProcessorConfig(dc.config.k8sClusterScraper)
	affinityProcessor, err := compliance.NewAffinityProcessor(affinityProcessorConfig)
	if err != nil {
		glog.Errorf("Failed during process affinity rules: %s", err)
	} else {
		entityDTOs = affinityProcessor.ProcessAffinityRules(entityDTOs)
	}

	glog.V(2).Infof("begin to generate service EntityDTOs.")
	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.config.k8sClusterScraper)
	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", svcDiscResult.Err())
	} else {
		entityDTOs = append(entityDTOs, svcDiscResult.Content()...)
	}

	glog.V(2).Infof("There are %d entityDTOs.", len(entityDTOs))

	return entityDTOs, nil
}
