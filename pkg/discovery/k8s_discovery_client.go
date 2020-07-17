package discovery

import (
	"fmt"
	"strings"
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
	// Strategy to aggregate Container utilization data on ContainerSpec entity
	containerUtilizationDataAggStrategy string
	// Strategy to aggregate Container usage data on ContainerSpec entity
	containerUsageDataAggStrategy string
}

func NewDiscoveryConfig(probeConfig *configs.ProbeConfig,
	targetConfig *configs.K8sTargetConfig, ValidationWorkers int,
	ValidationTimeoutSec int, containerUtilizationDataAggStrategy, containerUsageDataAggStrategy string) *DiscoveryClientConfig {
	return &DiscoveryClientConfig{
		probeConfig:                         probeConfig,
		targetConfig:                        targetConfig,
		ValidationWorkers:                   ValidationWorkers,
		ValidationTimeoutSec:                ValidationTimeoutSec,
		containerUtilizationDataAggStrategy: containerUtilizationDataAggStrategy,
		containerUsageDataAggStrategy:       containerUsageDataAggStrategy,
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
	k8sClusterScraper := cluster.NewClusterScraper(config.probeConfig.ClusterClient, config.probeConfig.DynamicClient)

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

	// Only add the following fields when target has been configured in kubeturbo
	if targetConf.TargetIdentifier != "" {
		masterHost := registration.MasterHost
		accVal = &proto.AccountValue{
			Key:         &masterHost,
			StringValue: &targetConf.MasterHost,
		}
		accountValues = append(accountValues, accVal)

		serverVersion := registration.ServerVersion
		version := strings.Join(targetConf.ServerVersions, ", ")
		accVal = &proto.AccountValue{
			Key:         &serverVersion,
			StringValue: &version,
		}
		accountValues = append(accountValues, accVal)

		image := registration.Image
		accVal = &proto.AccountValue{
			Key:         &image,
			StringValue: &targetConf.ProbeContainerImage,
		}
		accountValues = append(accountValues, accVal)

		imageID := registration.ImageID
		accVal = &proto.AccountValue{
			Key:         &imageID,
			StringValue: &targetConf.ProbeContainerImageID,
		}
		accountValues = append(accountValues, accVal)
	}

	targetInfo := sdkprobe.NewTurboTargetInfoBuilder(targetConf.ProbeCategory,
		targetConf.TargetType, targetID, accountValues).
		Create()
	return targetInfo
}

// Validate the Target
func (dc *K8sDiscoveryClient) Validate(
	accountValues []*proto.AccountValue) (validationResponse *proto.ValidationResponse, err error) {

	glog.V(2).Infof("Validating Kubernetes target...")

	defer func() {
		validationResponse = &proto.ValidationResponse{}
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
	}()

	var targetID string
	for _, accountValue := range accountValues {
		glog.V(4).Infof("%v", accountValue)
		if accountValue.GetKey() == registration.TargetIdentifierField {
			targetID = accountValue.GetStringValue()
		}
	}

	if targetID == "" {
		err = fmt.Errorf("empty target ID")
		return
	}

	if dc.clusterProcessor == nil {
		err = fmt.Errorf("null cluster processor")
		return
	}

	err = dc.clusterProcessor.ConnectCluster()
	return
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
// This is a part of the interface that gets registered with and is invoked asynchronously by the GO SDK Probe.
func (dc *K8sDiscoveryClient) Discover(
	accountValues []*proto.AccountValue) (discoveryResponse *proto.DiscoveryResponse, err error) {

	glog.V(2).Infof("Discovering kubernetes cluster...")

	discoveryResponse = &proto.DiscoveryResponse{}

	var targetID string
	for _, accountValue := range accountValues {
		glog.V(4).Infof("%v", accountValue)
		if accountValue.GetKey() == registration.TargetIdentifierField {
			targetID = accountValue.GetStringValue()
		}
	}

	if targetID == "" {
		glog.Errorf("Failed to discover kubernetes cluster: empty target ID")
		return
	}

	currentTime := time.Now()
	newDiscoveryResultDTOs, groupDTOs, err := dc.discoverWithNewFramework(targetID)
	if err != nil {
		glog.Errorf("Failed to discover kubernetes cluster: %v", err)
		return
	}

	discoveryResponse = &proto.DiscoveryResponse{
		DiscoveredGroup: groupDTOs,
		EntityDTO:       newDiscoveryResultDTOs,
	}

	newFrameworkDiscTime := time.Now().Sub(currentTime).Seconds()
	glog.V(2).Infof("Successfully discovered kubernetes cluster in %.3f seconds", newFrameworkDiscTime)

	return
}

/*
	The actual discovery work is done here.
*/
func (dc *K8sDiscoveryClient) discoverWithNewFramework(targetID string) ([]*proto.EntityDTO, []*proto.GroupDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES, QUOTAS, SERVICES HERE
	kubeCluster, err := dc.clusterProcessor.DiscoverCluster()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process cluster: %v", err)
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	// Multiple discovery workers to create node and pod DTOs
	nodes := clusterSummary.NodeList
	// Call cache cleanup
	dc.config.probeConfig.NodeClient.CleanupCache(nodes)

	// Discover pods and create DTOs for nodes, namespaces, controllers, pods, containers, application.
	// Collect the kubePod, kubeNamespace metrics, groups and kubeControllers from all the discovery workers
	workerCount := dc.dispatcher.Dispatch(nodes, clusterSummary)
	entityDTOs, podEntitiesMap, namespaceMetricsList, entityGroupList, kubeControllerList, containerSpecsList :=
		dc.resultCollector.Collect(workerCount)

	// Namespace discovery worker to create namespace DTOs
	stitchType := dc.config.probeConfig.StitchingPropertyType
	namespacesDiscoveryWorker := worker.Newk8sNamespaceDiscoveryWorker(clusterSummary, stitchType)
	namespaceDtos, err := namespacesDiscoveryWorker.Do(namespaceMetricsList)
	if err != nil {
		glog.Errorf("Failed to discover namespaces from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d namespace entityDTOs.", len(namespaceDtos))
		entityDTOs = append(entityDTOs, namespaceDtos...)
	}

	// K8s workload controller discovery worker to create WorkloadController DTOs
	controllerDiscoveryWorker := worker.NewK8sControllerDiscoveryWorker(clusterSummary)
	workloadControllerDtos, err := controllerDiscoveryWorker.Do(kubeControllerList)
	if err != nil {
		glog.Errorf("Failed to discover workload controllers from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d WorkloadController entityDTOs.", len(workloadControllerDtos))
		entityDTOs = append(entityDTOs, workloadControllerDtos...)
	}

	// K8s container spec discovery worker to create ContainerSpec DTOs by aggregating commodities data of container
	// replicas. ContainerSpec is an entity type which represents a certain type of container replicas deployed by a
	// K8s controller.
	containerSpecDiscoveryWorker := worker.NewK8sContainerSpecDiscoveryWorker()
	containerSpecDtos, err := containerSpecDiscoveryWorker.Do(containerSpecsList, dc.config.containerUtilizationDataAggStrategy,
		dc.config.containerUsageDataAggStrategy)
	if err != nil {
		glog.Errorf("Failed to discover ContainerSpecs from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d ContainerSpec entityDTOs", len(containerSpecDtos))
		entityDTOs = append(entityDTOs, containerSpecDtos...)
	}

	// Service DTOs
	glog.V(2).Infof("Begin to generate service EntityDTOs.")
	svcDiscWorker := worker.Newk8sServiceDiscoveryWorker(clusterSummary)
	serviceDtos, err := svcDiscWorker.Do(podEntitiesMap)
	if err != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d service entityDTOs.", len(serviceDtos))
		entityDTOs = append(entityDTOs, serviceDtos...)
	}

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
	// Also handles the creation of access commodities to handle unschedulable nodes
	glog.V(2).Infof("Begin to process taints and tolerations")
	nodesManager := compliance.NewNodeSchedulabilityManager(clusterSummary)

	taintTolerationProcessor, err := compliance.NewTaintTolerationProcessor(clusterSummary, nodesManager)
	if err != nil {
		glog.Errorf("Failed during process taints and tolerations: %v", err)
	} else {
		// Add access commodities to entity DOTs based on the taint-toleration rules
		taintTolerationProcessor.Process(entityDTOs)
	}

	// Anti-Affinity policy to prevent pods on unschedulable nodes to move to other unschedulable nodes
	nodesAntiAffinityGroupBuilder := compliance.NewUnschedulableNodesAntiAffinityGroupDTOBuilder(
		clusterSummary, targetID, nodesManager)
	nodeAntiAffinityGroupDTOs := nodesAntiAffinityGroupBuilder.Build()

	glog.V(2).Infof("Successfully processed taints and tolerations.")

	// Discovery worker for creating Group DTOs
	entityGroupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(clusterSummary, targetID)
	groupDTOs, _ := entityGroupDiscoveryWorker.Do(entityGroupList)

	glog.V(2).Infof("There are totally %d groups DTOs", len(groupDTOs))
	if glog.V(4) {
		for _, groupDto := range groupDTOs {
			glog.Infof("%s %s members: %+v",
				groupDto.GetDisplayName(), groupDto.GetGroupName(),
				groupDto.GetMemberList().Member)
		}
	}

	groupDTOs = append(groupDTOs, nodeAntiAffinityGroupDTOs...)

	return entityDTOs, groupDTOs, nil
}
