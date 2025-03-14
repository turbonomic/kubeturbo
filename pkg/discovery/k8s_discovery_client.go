package discovery

import (
	"fmt"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/golang/glog"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	sdkprobe "github.ibm.com/turbonomic/turbo-go-sdk/pkg/probe"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/processor"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/k8sappcomponents"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	"github.ibm.com/turbonomic/kubeturbo/pkg/registration"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	kubeturboversion "github.ibm.com/turbonomic/kubeturbo/version"
	api "k8s.io/api/core/v1"
)

const (
	minDiscoveryWorker = 1
	// Max number of data samples to be collected for each resource metric
	maxDataSamples = 60
	// Min sampling discovery interval
	minSampleIntervalSec = 10
)

type DiscoveryClientConfig struct {
	probeConfig                *configs.ProbeConfig //contains the k8s resources clients
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
	ORMClientManager *resourcemapping.ORMClientManager
	ormHandler       resourcemapping.ORMHandler
	// Number of workload controller items the list api call should request for
	itemsPerListQuery int
	// VCPU Throttling threshold
	CommodityConfig *dtofactory.CommodityConfig
}

func NewDiscoveryConfig(probeConfig *configs.ProbeConfig,
	targetConfig *configs.K8sTargetConfig, ValidationWorkers int,
	ValidationTimeoutSec int, containerUtilizationDataAggStrategy,
	containerUsageDataAggStrategy string, ormClientManager *resourcemapping.ORMClientManager,
	discoveryWorkers, discoveryTimeoutMin, discoverySamples,
	discoverySampleIntervalSec, itemsPerListQuery int) *DiscoveryClientConfig {
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
		ORMClientManager:                    ormClientManager,
		DiscoveryWorkers:                    discoveryWorkers,
		DiscoveryTimeoutSec:                 discoveryTimeoutMin,
		DiscoverySamples:                    discoverySamples,
		DiscoverySampleIntervalSec:          discoverySampleIntervalSec,
		itemsPerListQuery:                   itemsPerListQuery,
		ormHandler:                          ormClientManager,
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
		config.ValidationWorkers, config.ValidationTimeoutSec, config.itemsPerListQuery, config.targetConfig.IsOcp)

	globalEntityMetricSink := metrics.NewEntityMetricSink().WithMaxMetricPointsSize(config.DiscoverySamples)

	// make maxWorkerCount of result collector twice the worker count.
	resultCollector := worker.NewResultCollector(config.DiscoveryWorkers * 2)

	dispatcherConfig := worker.NewDispatcherConfig(k8sClusterScraper, config.probeConfig,
		config.DiscoveryWorkers, config.DiscoveryTimeoutSec, config.DiscoverySamples, config.DiscoverySampleIntervalSec).
		WithClusterKeyInjected(config.ClusterKeyInjected).
		WithORMHandler(&config.ormHandler)
	dispatcher := worker.NewDispatcher(dispatcherConfig, globalEntityMetricSink)
	dispatcher.Init(resultCollector)

	// Create new SamplingDispatcher to assign tasks to collect additional resource usage data samples from kubelet
	samplingDispatcherConfig := worker.NewDispatcherConfig(k8sClusterScraper, config.probeConfig,
		config.DiscoveryWorkers, config.DiscoverySampleIntervalSec, config.DiscoverySamples, config.DiscoverySampleIntervalSec).
		WithClusterKeyInjected(config.ClusterKeyInjected).
		WithORMHandler(&config.ormHandler)
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
		version := targetConf.ServerVersion
		if targetConf.IsOcp {
			version += ", OCP " + targetConf.ServerVersionOcp
		}
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

	targetInfo := sdkprobe.NewTurboTargetInfoBuilderWithSubType(targetConf.ProbeCategory,
		targetConf.TargetType, targetConf.TargetSubType, targetID, accountValues).
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

// DiscoverNodes queries API server to get the nodes in the cluster
func (dc *K8sDiscoveryClient) DiscoverNodes() ([]*api.Node, error) {
	return dc.clusterProcessor.GetAllNodes()
}

// StartSampling kicks off sampling cycles between full discoveries
func (dc *K8sDiscoveryClient) StartSampling(nodes []*api.Node) {
	dc.samplingDispatcher.ScheduleSamplingDiscoveries(nodes)
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

	var actionPolicies []*proto.ActionPolicyDTO
	if dc.Config.targetConfig.IsOcp {
		actionPolicies = dc.getTargetActionPolicies()
	} else {
		glog.V(2).Infof("Skip processing action policies for non-OpenShift cluster")
		actionPolicies = nil
	}
	discoveryResponse = &proto.DiscoveryResponse{
		DiscoveredGroup: groupDTOs,
		EntityDTO:       newDiscoveryResultDTOs,
		ActionPolicies:  actionPolicies,
	}

	newFrameworkDiscTime := time.Now().Sub(currentTime).Seconds()
	glog.V(2).Infof("Successfully discovered kubernetes cluster in %.3f seconds", newFrameworkDiscTime)

	return
}

// DiscoverWithNewFramework performs the actual discovery.
func (dc *K8sDiscoveryClient) DiscoverWithNewFramework(targetID string) ([]*proto.EntityDTO, []*proto.GroupDTO, error) {
	// CREATE CLUSTER, NODES, NAMESPACES, QUOTAS, SERVICES HERE
	start := time.Now()
	clusterSummary, err := dc.clusterProcessor.DiscoverCluster()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process cluster: %v", err)
	}
	glog.V(3).Infof("Discovering cluster resources took %s", time.Since(start))

	// affinity process with new algorithm
	var nodesPods map[string][]string
	var podsWithAffinities sets.String
	var hostnameSpreadWorkloads map[string]sets.String
	var otherSpreadPods sets.String
	var otherSpreadWorkloads map[string]sets.String
	var podNonHostnameAntiTermTopologyKeys map[string]sets.String
	var topologySpreadConstraintNodesToPods map[string]sets.String
	var affinityMapper *podaffinity.AffinityMapper
	if !utilfeature.DefaultFeatureGate.Enabled(features.IgnoreAffinities) {
		if utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) {
			glog.V(2).Infof("Begin to process affinity with new algorithm.")
			start := time.Now()
			// Create the stopCh for the entire discovery
			stopCh := make(chan struct{})
			// Encure stopCh is closed when this discovery finishes
			defer close(stopCh)
			namespaceLister, err := podaffinity.NewNamespaceLister(dc.k8sClusterScraper.Clientset, clusterSummary, stopCh)
			if err != nil {
				glog.Errorf("Error creating affinity processor: %v", err)
			} else {
				affinityProcessor, err := podaffinity.New(clusterSummary,
					podaffinity.NewNodeInfoLister(clusterSummary), namespaceLister)
				if err != nil {
					glog.Errorf("Failure in processing affinity rules: %s", err)
				} else {
					nodesPods, podsWithAffinities, hostnameSpreadWorkloads, otherSpreadPods, otherSpreadWorkloads,
						podNonHostnameAntiTermTopologyKeys, topologySpreadConstraintNodesToPods,
						affinityMapper = affinityProcessor.ProcessAffinities(clusterSummary.Pods)
				}
				glog.V(2).Infof("Successfully processed affinities.")
				glog.V(2).Infof("Processing affinities with new algorithm took %s", time.Since(start))
				if glog.V(3) {
					nodeCommsTotal := 0
					for node, pods := range nodesPods {
						nodeCommsTotal += len(pods)
						glog.Infof("Node %s will sell %v affinity related commodities.", node, len(pods))
					}
					glog.Infof("Total %v affinity related commodities will be sold by all nodes.", nodeCommsTotal)
				}
				glog.V(6).Infof("\n\nProcessed affinity result: \n\n %++v \n\n %++v \n\n",
					nodesPods, podsWithAffinities)
			}
		}
	} else {
		glog.V(2).Infof("Ignoring affinities.")
	}

	// ORM Discovery
	dc.Config.ORMClientManager.DiscoverORMs()

	// Multiple discovery workers to create node and pod DTOs
	nodes := clusterSummary.Nodes
	// Call cache cleanup
	dc.Config.probeConfig.NodeClient.CleanupCache(nodes)
	// Stops scheduling dispatcher to assign sampling discovery tasks.
	dc.samplingDispatcher.FinishSampling()

	var nodeGrpDTOs []*proto.EntityDTO
	var node2nodegroup map[string]sets.String
	if !utilfeature.DefaultFeatureGate.Enabled(features.IgnoreAffinities) {
		if utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) &&
			utilfeature.DefaultFeatureGate.Enabled(features.SegmentationBasedTopologySpread) {
			// NodeGroup DTOs
			glog.V(2).Infof("Begin to generate NodeGroup EntityDTOs.")
			start = time.Now()
			nodeGrpDTOs, node2nodegroup = dtofactory.
				NewNodeGroupEntityDTOBuilder(
					clusterSummary,
					otherSpreadWorkloads,
					getOtherSpreadTopologyKeys(podNonHostnameAntiTermTopologyKeys),
					affinityMapper,
				).BuildEntityDTOs()
			glog.V(3).Infof("Creating NodeGroup entityDTOs took %s", time.Since(start))
			glog.V(2).Infof("There are %d NodeGroup entityDTOs.", len(nodeGrpDTOs))
		}
	}

	start = time.Now()
	// Discover pods and create DTOs for nodes, namespaces, controllers, pods, containers, application.
	// Merge collected usage data samples from globalEntityMetricSink into the metric sink of each individual discovery worker.
	// Collect the kubePod, kubeNamespace metrics, groups and kubeControllers from all the discovery workers.
	taskCount := dc.dispatcher.Dispatch(nodes, nodesPods, podsWithAffinities, otherSpreadPods, hostnameSpreadWorkloads, clusterSummary,
		node2nodegroup, podNonHostnameAntiTermTopologyKeys, topologySpreadConstraintNodesToPods, affinityMapper)
	result := dc.resultCollector.Collect(taskCount)
	glog.V(3).Infof("Collection and processing of metrics from node kubelets took %s", time.Since(start))

	// Clear globalEntityMetricSink cache after collecting full discovery results
	dc.globalEntityMetricSink.ClearCache()
	// Reschedule dispatch sampling discovery tasks for newly discovered nodes
	dc.samplingDispatcher.ScheduleSamplingDiscoveries(nodes)

	start = time.Now()
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
	containerSpecDiscoveryWorker := worker.NewK8sContainerSpecDiscoveryWorker(dc.Config.CommodityConfig).
		WithORMHandler(dc.Config.ormHandler)
	containerSpecDtos, operatorControlledContainerSpecs, systemNameSpaceContainerSpecs, unresizableContainerSpecs := containerSpecDiscoveryWorker.
		Do(clusterSummary, result.ContainerSpecMetrics, dc.Config.containerUtilizationDataAggStrategy, dc.Config.containerUsageDataAggStrategy)
	glog.V(2).Infof("There are %d ContainerSpec entityDTOs", len(containerSpecDtos))
	result.EntityDTOs = append(result.EntityDTOs, containerSpecDtos...)

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
	glog.V(3).Infof("Postprocessing and aggregatig DTOs took %s", time.Since(start))

	// affinity process
	if !utilfeature.DefaultFeatureGate.Enabled(features.IgnoreAffinities) &&
		!utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) {
		glog.V(2).Infof("Begin to process affinity.")
		affinityProcessor, err := compliance.NewAffinityProcessor(clusterSummary)
		if err != nil {
			glog.Errorf("Failed during process affinity rules: %s", err)
		} else {
			result.EntityDTOs = affinityProcessor.ProcessAffinityRules(result.EntityDTOs)
		}
		glog.V(2).Infof("Successfully processed affinity.")
	}

	// Taint-toleration process to create access commodities
	glog.V(2).Infof("Begin to process taints and tolerations")
	start = time.Now()
	taintTolerationProcessor, err := compliance.NewTaintTolerationProcessor(clusterSummary)
	if err != nil {
		glog.Errorf("Failed during process taints and tolerations: %v", err)
	} else {
		// Add access commodities to entity DOTs based on the taint-toleration rules
		taintTolerationProcessor.Process(result.EntityDTOs)
		glog.V(3).Infof("Processing taints took %s", time.Since(start))
	}

	glog.V(2).Infof("Successfully processed taints and tolerations.")

	// Appending
	result.EntityDTOs = append(result.EntityDTOs, nodeGrpDTOs...)

	// Discovery worker for creating Group DTOs
	entityGroupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(clusterSummary, targetID)
	groupDTOs, _ := entityGroupDiscoveryWorker.Do(result.EntityGroups, result.SidecarContainerSpecs,
		operatorControlledContainerSpecs, systemNameSpaceContainerSpecs, result.PodsWithVolumes, result.NotReadyNodes,
		result.MirrorPodUids, result.KubeVirtPodUids, result.KubeVirtContainerSpecUids, topologySpreadConstraintNodesToPods,
		result.UnmovablePods, unresizableContainerSpecs)

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

	// Log the evidence for topology spread constraints
	dc.logEvidenceForTopologySpreadConstraints(clusterSummary)

	return result.EntityDTOs, groupDTOs, nil
}

func (dc *K8sDiscoveryClient) getTargetActionPolicies() []*proto.ActionPolicyDTO {
	isEnabled, err := dc.k8sClusterScraper.IsClusterAPIEnabled()
	if !isEnabled {
		glog.V(2).Infof("Do not set node action policy for this cluster. %s", err)
		return nil
	}

	// Set target level action policy for virtual machine entity type if cluster API is enabled
	glog.V(2).Info("Cluster API is available. Set node action policy for this cluster.")
	entityType := proto.EntityDTO_VIRTUAL_MACHINE
	return builder.NewActionPolicyBuilder().
		WithEntityActionsThatSupportBatching(entityType, proto.ActionItemDTO_SUSPEND, proto.ActionPolicyDTO_SUPPORTED).
		WithEntityActions(entityType, proto.ActionItemDTO_PROVISION, proto.ActionPolicyDTO_SUPPORTED).
		Create()
}

func (dc *K8sDiscoveryClient) logEvidenceForTopologySpreadConstraints(clusterSummary *repository.ClusterSummary) {
	wlcWithTopologySpreadConstraints := make(map[string]*api.Pod)

	for _, pod := range clusterSummary.Pods {
		if len(pod.Spec.TopologySpreadConstraints) > 0 {
			wlcName, _ := clusterSummary.PodToControllerMap[pod.Namespace+"/"+pod.Name]
			if _, ok := wlcWithTopologySpreadConstraints[wlcName]; !ok {
				wlcWithTopologySpreadConstraints[wlcName] = pod
			}
		}
	}

	if len(wlcWithTopologySpreadConstraints) > 0 {
		glog.V(2).Infof("There is a total of %v workload controllers with topology spread constraints in this cluster.", len(wlcWithTopologySpreadConstraints))
		if glog.V(3) {
			for wlcName, apod := range wlcWithTopologySpreadConstraints {
				glog.Infof("%v is having topology spread constrains as %+v", wlcName, apod.Spec.TopologySpreadConstraints)
			}
		}
	}
}

func getOtherSpreadTopologyKeys(podsToTopologyKeys map[string]sets.String) sets.String {
	result := sets.NewString()
	for _, keys := range podsToTopologyKeys {
		for key := range keys {
			result.Insert(key)
		}
	}
	return result
}
