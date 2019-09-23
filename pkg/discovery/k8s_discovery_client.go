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
	probeConfig          *configs.ProbeConfig
	targetConfig         *configs.K8sTargetConfig
	ValidationWorkers    int
	ValidationTimeoutSec int
}

func NewDiscoveryConfig(probeConfig *configs.ProbeConfig,
	targetConfig *configs.K8sTargetConfig, ValidationWorkers int,
	ValidationTimeoutSec int) *DiscoveryClientConfig {
	return &DiscoveryClientConfig{
		probeConfig:          probeConfig,
		targetConfig:         targetConfig,
		ValidationWorkers:    ValidationWorkers,
		ValidationTimeoutSec: ValidationTimeoutSec,
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
	clusterProcessor := processor.NewClusterProcessor(k8sClusterScraper, config.probeConfig.NodeClient, config.ValidationWorkers, config.ValidationTimeoutSec)
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
		err = fmt.Errorf("null cluster processor")
	} else {
		err = dc.clusterProcessor.ConnectCluster()
	}
	if err != nil {
		glog.Errorf("Failed to validate target: %v.", err)
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
		glog.V(2).Infof("Successfully validated target.")
	}

	return validationResponse, nil
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
// This is a part of the interface that gets registered with and is invoked asynchronously by the GO SDK Probe.
func (dc *K8sDiscoveryClient) Discover(accountValues []*proto.AccountValue) (*proto.DiscoveryResponse, error) {
	glog.V(2).Infof("Discovering kubernetes cluster...")
	currentTime := time.Now()
	newDiscoveryResultDTOs, groupDTOs, err := dc.discoverWithNewFramework()
	if err != nil {
		glog.Errorf("Failed to discover kubernetes cluster: %v", err)
	}

	discoveryResponse := &proto.DiscoveryResponse{
		DiscoveredGroup: groupDTOs,
		EntityDTO:       newDiscoveryResultDTOs,
	}

	newFrameworkDiscTime := time.Now().Sub(currentTime).Seconds()
	glog.V(2).Infof("Successfully discovered kubernetes cluster in %.3f seconds", newFrameworkDiscTime)

	return discoveryResponse, nil
}

/*
	The actual discovery work is done here.
*/
func (dc *K8sDiscoveryClient) discoverWithNewFramework() ([]*proto.EntityDTO, []*proto.GroupDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES AND QUOTAS HERE
	kubeCluster, err := dc.clusterProcessor.DiscoverCluster()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process cluster: %v", err)
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	// Multiple discovery workers to create node and pod DTOs
	nodes := clusterSummary.NodeList
	// Call cache cleanup
	dc.config.probeConfig.NodeClient.CleanupCache(nodes)

	// Discover pods and create DTOs for nodes, pods, containers, application.
	// Collect the kubePod, quota metrics, groups from all the discovery workers
	workerCount := dc.dispatcher.Dispatch(nodes, clusterSummary)
	entityDTOs, podEntitiesMap, quotaMetricsList, policyGroupList := dc.resultCollector.Collect(workerCount)

	// Quota discovery worker to create quota DTOs
	stitchType := dc.config.probeConfig.StitchingPropertyType
	quotasDiscoveryWorker := worker.Newk8sResourceQuotasDiscoveryWorker(clusterSummary, stitchType)
	quotaDtos, _ := quotasDiscoveryWorker.Do(quotaMetricsList)

	// Service DTOs
	glog.V(2).Infof("Begin to generate service EntityDTOs.")
	svcDiscWorker := worker.Newk8sServiceDiscoveryWorker(clusterSummary)
	serviceDtos, err := svcDiscWorker.Do(podEntitiesMap)
	if err != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d vApp entityDTOs.", len(serviceDtos))
		entityDTOs = append(entityDTOs, serviceDtos...)
	}

	// All the DTOs
	entityDTOs = append(entityDTOs, quotaDtos...)

	glog.V(2).Infof("There are totally %d entityDTOs.", len(entityDTOs))

	// affinity process
	glog.V(2).Infof("Begin to process affinity.")
	affinityProcessorConfig := compliance.NewAffinityProcessorConfig(dc.k8sClusterScraper)
	affinityProcessor, err := compliance.NewAffinityProcessor(affinityProcessorConfig)
	if err != nil {
		glog.Errorf("Failed during process affinity rules: %s", err)
	} else {
		entityDTOs = affinityProcessor.ProcessAffinityRules(entityDTOs)
	}
	glog.V(2).Infof("Successfully processed affinity.")

	// Taint-toleration process to create access commodities
	glog.V(2).Infof("Begin to process taints and tolerations")
	taintTolerationProcessor, err := compliance.NewTaintTolerationProcessor(dc.k8sClusterScraper)
	if err != nil {
		glog.Errorf("Failed during process taints and tolerations: %v", err)
	} else {
		// Add access commodiites to entity DOTs based on the taint-toleration rules
		taintTolerationProcessor.Process(entityDTOs)
	}
	glog.V(2).Infof("Successfully processed taints and tolerations.")

	// Discovery worker for creating Group DTOs
	targetId := dc.config.targetConfig.TargetIdentifier
	entityGroupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(clusterSummary, targetId)
	groupDTOs, _ := entityGroupDiscoveryWorker.Do(policyGroupList)

	glog.V(2).Infof("There are totally %d groups DTOs", len(groupDTOs))
	if glog.V(4) {
		for _, groupDto := range groupDTOs {
			glog.Infof("%s %s members: %+v",
				groupDto.GetDisplayName(), groupDto.GetGroupName(),
				groupDto.GetMemberList().Member)
		}
	}

	return entityDTOs, groupDTOs, nil
}
