package discovery

import (
	"fmt"
	"time"

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
	probeConfig  *configs.ProbeConfig
	targetConfig *configs.K8sTargetConfig
}

func NewDiscoveryConfig(probeConfig *configs.ProbeConfig,
	targetConfig *configs.K8sTargetConfig) *DiscoveryClientConfig {
	return &DiscoveryClientConfig{
		probeConfig:  probeConfig,
		targetConfig: targetConfig,
	}
}

// Implements the go sdk discovery client interface
type K8sDiscoveryClient struct {
	config            *DiscoveryClientConfig
	k8sClusterScraper *cluster.ClusterScraper

	clusterProcessor *processor.ClusterProcessor
	dispatcher       *worker.Dispatcher
	resultCollector  *worker.ResultCollector
}

func NewK8sDiscoveryClient(config *DiscoveryClientConfig) *K8sDiscoveryClient {
	k8sClusterScraper := cluster.NewClusterScraper(config.probeConfig.ClusterClient)

	// for discovery tasks
	clusterProcessor := processor.NewClusterProcessor(k8sClusterScraper, config.probeConfig.NodeClient)
	// make maxWorkerCount of result collector twice the worker count.
	resultCollector := worker.NewResultCollector(workerCount * 2)

	dispatcherConfig := worker.NewDispatcherConfig(k8sClusterScraper, config.probeConfig, workerCount)
	dispatcher := worker.NewDispatcher(dispatcherConfig)
	dispatcher.Init(resultCollector)

	dc := &K8sDiscoveryClient{
		config:            config,
		k8sClusterScraper: k8sClusterScraper,
		clusterProcessor:  clusterProcessor,
		dispatcher:        dispatcher,
		resultCollector:   resultCollector,
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

	targetInfo := sdkprobe.NewTurboTargetInfoBuilder(targetConf.ProbeCategory,
		targetConf.TargetType, targetID, accountValues).
		Create()
	return targetInfo
}

// Validate the Target
func (dc *K8sDiscoveryClient) Validate(accountValues []*proto.AccountValue) (*proto.ValidationResponse, error) {
	glog.V(2).Infof("Validating Kubernetes target...")

	validationResponse := &proto.ValidationResponse{}

	var err error
	if dc.clusterProcessor == nil {
		err = fmt.Errorf("Null cluster processor")
	} else {
		err = dc.clusterProcessor.ConnectCluster()
	}
	if err != nil {
		errStr := fmt.Sprintf("%s\n", err)
		severity := proto.ErrorDTO_CRITICAL
		var errorDtos []*proto.ErrorDTO
		errorDto := &proto.ErrorDTO{
			Severity:    &severity,
			Description: &errStr,
		}
		errorDtos = append(errorDtos, errorDto)
		validationResponse.ErrorDTO = errorDtos
	} else {
		glog.V(2).Infof("Validation response - connected to cluster\n")
	}

	return validationResponse, nil
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
// This is a part of the interface that gets registered with and is invoked asynchronously by the GO SDK Probe.
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

/*
	The actual discovery work is done here.
*/
func (dc *K8sDiscoveryClient) discoverWithNewFramework() ([]*proto.EntityDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES AND QUOTAS HERE
	kubeCluster, err := dc.clusterProcessor.DiscoverCluster()
	if err != nil {
		return nil, fmt.Errorf("Failed to process cluster: %s", err)
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	// Multiple discovery workers to create node and pod DTOs
	nodes := clusterSummary.NodeList
	workerCount := dc.dispatcher.Dispatch(nodes, clusterSummary)
	entityDTOs, quotaMetricsList := dc.resultCollector.Collect(workerCount)

	// Quota discovery worker to create quota DTOs
	stitchType := dc.config.probeConfig.StitchingPropertyType
	quotasDiscoveryWorker := worker.Newk8sResourceQuotasDiscoveryWorker(clusterSummary, stitchType)
	quotaDtos, _ := quotasDiscoveryWorker.Do(quotaMetricsList)

	// All the DTOs
	entityDTOs = append(entityDTOs, quotaDtos...)
	glog.V(2).Infof("Discovery workers have finished discovery work with %d entityDTOs built.", len(entityDTOs))

	// affinity process
	glog.V(2).Infof("Begin to process affinity.")
	affinityProcessorConfig := compliance.NewAffinityProcessorConfig(dc.k8sClusterScraper)
	affinityProcessor, err := compliance.NewAffinityProcessor(affinityProcessorConfig)
	if err != nil {
		glog.Errorf("Failed during process affinity rules: %s", err)
	} else {
		entityDTOs = affinityProcessor.ProcessAffinityRules(entityDTOs)
	}

	glog.V(2).Infof("begin to generate service EntityDTOs.")
	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.k8sClusterScraper)
	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", svcDiscResult.Err())
	} else {
		glog.V(2).Infof("There are %d vApp entityDTOs.", len(svcDiscResult.Content()))
		entityDTOs = append(entityDTOs, svcDiscResult.Content()...)
	}

	glog.V(2).Infof("There are %d entityDTOs.", len(entityDTOs))

	return entityDTOs, nil
}
