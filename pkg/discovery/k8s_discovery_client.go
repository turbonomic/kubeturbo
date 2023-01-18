package discovery

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/golang/glog"
	sdkprobe "github.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/processor"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/k8sappcomponents"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/registration"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	kubeturboversion "github.com/turbonomic/kubeturbo/version"
)

const (
	minDiscoveryWorker = 1
	// Max number of data samples to be collected for each resource metric
	maxDataSamples = 60
	// Min sampling discovery interval
	minSampleIntervalSec = 10
)

type DiscoveryClientConfig struct {
	probeConfig                *configs.ProbeConfig
	targetConfig               *configs.K8sTargetConfig
	ValidationWorkers          int
	ValidationTimeoutSec       int
	DiscoveryWorkers           int
	DiscoveryTimeoutSec        int
	DiscoverySamples           int
	DiscoverySampleIntervalSec int
	ClusterKeyInjected         string
	// Strategy to aggregate Container utilization data on ContainerSpec entity
	containerUtilizationDataAggStrategy string
	// Strategy to aggregate Container usage data on ContainerSpec entity
	containerUsageDataAggStrategy string
	// ORMClient builds operator resource mapping templates fetched from OperatorResourceMapping CR so that action
	// execution client will be able to execute action on operator-managed resources based on resource mapping templates.
	OrmClient *resourcemapping.ORMClient
	// Number of workload controller items the list api call should request for
	itemsPerListQuery int
	// VCPU Throttling threshold
	CommodityConfig *dtofactory.CommodityConfig
}

func NewDiscoveryConfig(probeConfig *configs.ProbeConfig,
	targetConfig *configs.K8sTargetConfig, ValidationWorkers int,
	ValidationTimeoutSec int, containerUtilizationDataAggStrategy,
	containerUsageDataAggStrategy string, ormClient *resourcemapping.ORMClient,
	discoveryWorkers, discoveryTimeoutMin, discoverySamples,
	discoverySampleIntervalSec, itemsPerListQuery int, commodityConfig *dtofactory.CommodityConfig) *DiscoveryClientConfig {
	if discoveryWorkers < minDiscoveryWorker {
		glog.Warningf("Invalid number of discovery workers %v, set it to %v.",
			discoveryWorkers, minDiscoveryWorker)
		discoveryWorkers = minDiscoveryWorker
	} else {
		glog.Infof("Number of discovery workers: %v.", discoveryWorkers)
	}
	if discoverySamples > maxDataSamples {
		glog.Warningf("Number of discovery samples %v is higher than %v, set it to %v.", discoverySamples,
			maxDataSamples, maxDataSamples)
		discoverySamples = maxDataSamples
	}
	if discoverySampleIntervalSec < minSampleIntervalSec {
		glog.Warningf("Sampling discovery interval %v seconds is lower than %v seconds, set it to %v seconds.", discoverySampleIntervalSec,
			minSampleIntervalSec, minSampleIntervalSec)
		discoverySampleIntervalSec = minSampleIntervalSec
	}
	return &DiscoveryClientConfig{
		probeConfig:                         probeConfig,
		targetConfig:                        targetConfig,
		ValidationWorkers:                   ValidationWorkers,
		ValidationTimeoutSec:                ValidationTimeoutSec,
		containerUtilizationDataAggStrategy: containerUtilizationDataAggStrategy,
		containerUsageDataAggStrategy:       containerUsageDataAggStrategy,
		OrmClient:                           ormClient,
		DiscoveryWorkers:                    discoveryWorkers,
		DiscoveryTimeoutSec:                 discoveryTimeoutMin,
		DiscoverySamples:                    discoverySamples,
		DiscoverySampleIntervalSec:          discoverySampleIntervalSec,
		itemsPerListQuery:                   itemsPerListQuery,
		CommodityConfig:                     commodityConfig,
	}
}

// WithClusterKeyInjected sets the clusterKeyInjected for the DiscoveryClientConfig.
func (config *DiscoveryClientConfig) WithClusterKeyInjected(clusterKeyInjected string) *DiscoveryClientConfig {
	config.ClusterKeyInjected = clusterKeyInjected
	return config
}

// K8sDiscoveryClient defines the go sdk discovery client interface
type K8sDiscoveryClient struct {
	Config                 *DiscoveryClientConfig
	k8sClusterScraper      *cluster.ClusterScraper
	clusterProcessor       *processor.ClusterProcessor
	dispatcher             *worker.Dispatcher
	samplingDispatcher     *worker.SamplingDispatcher
	resultCollector        *worker.ResultCollector
	globalEntityMetricSink *metrics.EntityMetricSink
}

func NewK8sDiscoveryClient(config *DiscoveryClientConfig) *K8sDiscoveryClient {
	k8sClusterScraper := config.probeConfig.ClusterScraper

	// for discovery tasks
	clusterProcessor := processor.NewClusterProcessor(k8sClusterScraper, config.probeConfig.NodeClient,
		config.ValidationWorkers, config.ValidationTimeoutSec, config.itemsPerListQuery)

	globalEntityMetricSink := metrics.NewEntityMetricSink().WithMaxMetricPointsSize(config.DiscoverySamples)

	// make maxWorkerCount of result collector twice the worker count.
	resultCollector := worker.NewResultCollector(config.DiscoveryWorkers * 2)

	dispatcherConfig := worker.NewDispatcherConfig(k8sClusterScraper, config.probeConfig,
		config.DiscoveryWorkers, config.DiscoveryTimeoutSec, config.DiscoverySamples, config.DiscoverySampleIntervalSec,
		config.CommodityConfig).
		WithClusterKeyInjected(config.ClusterKeyInjected)
	dispatcher := worker.NewDispatcher(dispatcherConfig, globalEntityMetricSink)
	dispatcher.Init(resultCollector)

	// Create new SamplingDispatcher to assign tasks to collect additional resource usage data samples from kubelet
	samplingDispatcherConfig := worker.NewDispatcherConfig(k8sClusterScraper, config.probeConfig,
		config.DiscoveryWorkers, config.DiscoverySampleIntervalSec, config.DiscoverySamples, config.DiscoverySampleIntervalSec,
		config.CommodityConfig).
		WithClusterKeyInjected(config.ClusterKeyInjected)
	dataSamplingDispatcher := worker.NewSamplingDispatcher(samplingDispatcherConfig, globalEntityMetricSink)
	dataSamplingDispatcher.InitSamplingDiscoveryWorkers()

	dc := &K8sDiscoveryClient{
		Config:                 config,
		k8sClusterScraper:      k8sClusterScraper,
		clusterProcessor:       clusterProcessor,
		dispatcher:             dispatcher,
		samplingDispatcher:     dataSamplingDispatcher,
		resultCollector:        resultCollector,
		globalEntityMetricSink: globalEntityMetricSink,
	}
	return dc
}

func (dc *K8sDiscoveryClient) GetAccountValues() *sdkprobe.TurboTargetInfo {
	var accountValues []*proto.AccountValue
	targetConf := dc.Config.targetConfig
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

		probeVersion := registration.ProbeVersion
		accVal = &proto.AccountValue{
			Key:         &probeVersion,
			StringValue: &kubeturboversion.Version,
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

// Discover topology receives a discovery request from server and start probing the k8s.
// This is a part of the interface that gets registered with and is invoked asynchronously by the GO SDK Probe.
func (dc *K8sDiscoveryClient) Discover(
	accountValues []*proto.AccountValue) (discoveryResponse *proto.DiscoveryResponse, err error) {

	glog.V(2).Infof("Discovering kubernetes cluster...")

	if utilfeature.DefaultFeatureGate.Enabled(features.GoMemLimit) {
		// Set Go runtime soft memory limit: https://pkg.go.dev/runtime/debug#SetMemoryLimit
		// Set Go runtime soft memory limit through the AUTOMEMLIMIT environment variable.
		// AUTOMEMLIMIT configures how much memory of the cgroup's memory limit should be set as Go runtime
		// soft memory limit in the half-open range (0.0,1.0].
		// If AUTOMEMLIMIT is not set, it defaults to 0.9. This means 10% is the headroom for memory sources
		// that the Go runtime is unaware of and unable to control.
		// If GOMEMLIMIT environment variable is already set or AUTOMEMLIMIT=off, this function does nothing.
		// Set GoMemLimit during each discovery as memory limit may change over time (when in-place resource
		// update is enabled).
		memlimit.SetGoMemLimitWithEnv()
	}

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
	newDiscoveryResultDTOs, groupDTOs, err := dc.DiscoverWithNewFramework(targetID)
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

// DiscoverWithNewFramework performs the actual discovery.
func (dc *K8sDiscoveryClient) DiscoverWithNewFramework(targetID string) ([]*proto.EntityDTO, []*proto.GroupDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES, QUOTAS, SERVICES HERE
	clusterSummary, err := dc.clusterProcessor.DiscoverCluster()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process cluster: %v", err)
	}

	// Build the placement map in parallel
	var wg sync.WaitGroup
	if !utilfeature.DefaultFeatureGate.Enabled(features.IgnoreAffinities) {
		glog.V(2).Infof("Begin to process affinity.")
		affinityHandler, err := compliance.NewAffinityProcessor(clusterSummary)
		if err != nil {
			glog.Errorf("Failed during process affinity rules: %s", err)
		} else {
			wg.Add(1)
			go affinityHandler.ProcessAffinityRules(&wg)
		}
	} else {
		glog.V(2).Infof("Ignoring affinities.")
	}

	// Cache operatorResourceSpecMap in ormClient
	numCRs := dc.Config.OrmClient.CacheORMSpecMap()
	if numCRs > 0 {
		glog.Infof("Discovered %v Operator managed Custom Resources in cluster %s.", numCRs, targetID)
	}

	// Multiple discovery workers to create node and pod DTOs
	nodes := clusterSummary.Nodes
	// Call cache cleanup
	dc.Config.probeConfig.NodeClient.CleanupCache(nodes)
	// Stops scheduling dispatcher to assign sampling discovery tasks.
	dc.samplingDispatcher.FinishSampling()

	// Discover pods and create DTOs for nodes, namespaces, controllers, pods, containers, application.
	// Merge collected usage data samples from globalEntityMetricSink into the metric sink of each individual discovery worker.
	// Collect the kubePod, kubeNamespace metrics, groups and kubeControllers from all the discovery workers.
	taskCount := dc.dispatcher.Dispatch(nodes, clusterSummary)
	result := dc.resultCollector.Collect(taskCount)

	// Clear globalEntityMetricSink cache after collecting full discovery results
	dc.globalEntityMetricSink.ClearCache()
	// Reschedule dispatch sampling discovery tasks for newly discovered nodes
	dc.samplingDispatcher.ScheduleDispatch(nodes)

	// Namespace discovery worker to create namespace DTOs
	stitchType := dc.Config.probeConfig.StitchingPropertyType
	namespacesDiscoveryWorker := worker.Newk8sNamespaceDiscoveryWorker(clusterSummary, stitchType)
	namespaceDtos, err := namespacesDiscoveryWorker.Do(result.NamespaceMetrics)
	if err != nil {
		glog.Errorf("Failed to discover namespaces from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d namespace entityDTOs.", len(namespaceDtos))
		result.EntityDTOs = append(result.EntityDTOs, namespaceDtos...)
	}

	// K8s workload controller discovery worker to create WorkloadController DTOs
	controllerDiscoveryWorker := worker.NewK8sControllerDiscoveryWorker(clusterSummary)
	workloadControllerDtos, err := controllerDiscoveryWorker.Do(clusterSummary, result.KubeControllers)
	if err != nil {
		glog.Errorf("Failed to discover workload controllers from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d WorkloadController entityDTOs.", len(workloadControllerDtos))
		result.EntityDTOs = append(result.EntityDTOs, workloadControllerDtos...)
	}

	// K8s container spec discovery worker to create ContainerSpec DTOs by aggregating commodities data of container
	// replicas. ContainerSpec is an entity type which represents a certain type of container replicas deployed by a
	// K8s controller.
	containerSpecDiscoveryWorker := worker.NewK8sContainerSpecDiscoveryWorker(dc.Config.CommodityConfig)
	containerSpecDtos, err := containerSpecDiscoveryWorker.Do(clusterSummary, result.ContainerSpecMetrics, dc.Config.containerUtilizationDataAggStrategy,
		dc.Config.containerUsageDataAggStrategy)
	if err != nil {
		glog.Errorf("Failed to discover ContainerSpecs from current Kubernetes cluster with the new discovery framework: %s", err)
	} else {
		glog.V(2).Infof("There are %d ContainerSpec entityDTOs", len(containerSpecDtos))
		result.EntityDTOs = append(result.EntityDTOs, containerSpecDtos...)
	}

	// Service DTOs
	glog.V(2).Infof("Begin to generate service EntityDTOs.")
	serviceDTOs := dtofactory.
		NewServiceEntityDTOBuilder(clusterSummary, dc.k8sClusterScraper, result.PodEntitiesMap).
		BuildDTOs()
	result.EntityDTOs = append(result.EntityDTOs, serviceDTOs...)
	glog.V(2).Infof("There are %d service entityDTOs.", len(serviceDTOs))

	if utilfeature.DefaultFeatureGate.Enabled(features.PersistentVolumes) {
		glog.V(2).Infof("Begin to generate persistent volume EntityDTOs.")
		// Persistent Volume DTOs
		volumeEntityDTOBuilder := dtofactory.NewVolumeEntityDTOBuilder(result.PodVolumeMetrics)
		volumeEntityDTOs, err := volumeEntityDTOBuilder.BuildEntityDTOs(clusterSummary.VolumeToPodsMap)
		if err != nil {
			glog.Errorf("Error while creating volume entityDTOs: %v", err)
		} else {
			glog.V(2).Infof("There are %d Storage Volume entityDTOs.", len(volumeEntityDTOs))
			result.EntityDTOs = append(result.EntityDTOs, volumeEntityDTOs...)
		}
	}

	k8sappcomponents.NewK8sAppComponentsProcessor(clusterSummary.ComponentToAppMap).
		ProcessAppComponentDTOs(result.EntityDTOs)
	businessAppEntityDTOBuilder := dtofactory.NewBusinessAppEntityDTOBuilder(clusterSummary.K8sAppToComponentMap)
	businessAppEntityDTOBuilderEntityDTOs := businessAppEntityDTOBuilder.BuildEntityDTOs()
	result.EntityDTOs = append(result.EntityDTOs, businessAppEntityDTOBuilderEntityDTOs...)

	glog.V(2).Infof("There are totally %d entityDTOs.", len(result.EntityDTOs))

	wg.Wait()
	// MergeAffinitiesToDTOs
	for podName, nodeLst := range compliance.Pod2NodesMapBasedOnAffinity {
		nodesStr := strings.Join(nodeLst, ",")
		glog.V(2).Infof("Based on affinity rules, pod<%v> could be placed on the node<%v>", podName, nodesStr)
	}

	// Taint-toleration process to create access commodities
	glog.V(2).Infof("Begin to process taints and tolerations")
	taintTolerationProcessor, err := compliance.NewTaintTolerationProcessor(clusterSummary)
	if err != nil {
		glog.Errorf("Failed during process taints and tolerations: %v", err)
	} else {
		// Add access commodities to entity DOTs based on the taint-toleration rules
		taintTolerationProcessor.Process(result.EntityDTOs)
	}

	glog.V(2).Infof("Successfully processed taints and tolerations.")

	// Discovery worker for creating Group DTOs
	entityGroupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(clusterSummary, targetID)
	groupDTOs, _ := entityGroupDiscoveryWorker.Do(result.EntityGroups, result.SidecarContainerSpecs,
		result.PodsWithVolumes, result.NotReadyNodes, result.MirrorPodUids)

	glog.V(2).Infof("There are totally %d groups DTOs", len(groupDTOs))
	if glog.V(4) {
		for _, groupDto := range groupDTOs {
			glog.Infof("%s %s members: %+v",
				groupDto.GetDisplayName(), groupDto.GetGroupName(),
				groupDto.GetMemberList().Member)
		}
	}

	// Create the cluster DTO
	clusterEntityDTO, err := dtofactory.NewClusterDTOBuilder(clusterSummary, targetID).BuildEntity(result.EntityDTOs, namespaceDtos)
	if err != nil {
		glog.Errorf("Failed to create the cluster DTO: %s", err)
	} else {
		glog.V(2).Infof("The cluster DTO has been created successfully: %+v", clusterEntityDTO)
		result.EntityDTOs = append(result.EntityDTOs, clusterEntityDTO)
	}

	return result.EntityDTOs, groupDTOs, nil
}
