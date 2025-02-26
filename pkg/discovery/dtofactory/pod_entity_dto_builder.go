package dtofactory

import (
	"fmt"
	"strings"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"

	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/golang/glog"
)

const (
	applicationCommodityDefaultCapacity = 1e10
	zoneLabelName                       = "topology.kubernetes.io/zone"
	regionLabelName                     = "topology.kubernetes.io/region"
)

var (
	pendingPodResCommTypeSold = []metrics.ResourceType{
		metrics.CPURequest,
		metrics.MemoryRequest,
	}

	pendingPodResCommTypeBoughtFromNode = []metrics.ResourceType{
		metrics.CPURequest,
		metrics.MemoryRequest,
	}

	runningPodResCommTypeSold = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
	}

	runningPodResCommTypeBoughtFromNode = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
		metrics.NumPods,
		metrics.VStorage,
		// TODO, add back provisioned commodity later
	}

	podQuotaCommType = []metrics.ResourceType{
		metrics.CPULimitQuota,
		metrics.MemoryLimitQuota,
		metrics.CPURequestQuota,
		metrics.MemoryRequestQuota,
	}

	horizontallyScalableControllerKinds = sets.NewString(
		commonutil.KindDeployment,
		commonutil.KindDeploymentConfig,
		commonutil.KindReplicaSet,
		commonutil.KindReplicationController,
		commonutil.KindStatefulSet,
		commonutil.KindVirtualMachineInstance,
	)

	horizontallScalableOperatorControlledControllerKinds = sets.NewString(
		commonutil.KindDeployment,
		commonutil.KindStatefulSet,
	)

	movableControllerKinds = sets.NewString(
		commonutil.KindReplicaSet,
		commonutil.KindReplicationController,
	)

	controllersWithParents = sets.NewString(
		commonutil.KindReplicaSet,
		commonutil.KindReplicationController,
	)
)

type podEntityDTOBuilder struct {
	generalBuilder
	stitchingManager                   *stitching.StitchingManager
	nodeNameUIDMap                     map[string]string
	namespaceUIDMap                    map[string]string
	podToVolumesMap                    map[string][]repository.MountedVolume
	nodeNameToNodeMap                  map[string]*repository.KubeNode
	runningPods                        []*api.Pod
	pendingPods                        []*api.Pod
	clusterKeyInjected                 string
	mirrorPodToDaemonMap               map[string]bool
	ClusterScraper                     *cluster.ClusterScraper
	podsWithLabelBasedAffinities       sets.String
	hostnameSpreadPods                 sets.String
	otherSpreadPods                    sets.String
	podsToControllers                  map[string]string
	podNonHostnameAntiTermTopologyKeys map[string]sets.String
	node2nodegroups                    map[string]sets.String
	affinityMapper                     *podaffinity.AffinityMapper
	ormHandler                         resourcemapping.ORMHandler
}

func NewPodEntityDTOBuilder(sink *metrics.EntityMetricSink, cluster *repository.ClusterSummary, stitchingManager *stitching.StitchingManager, clusterScraper *cluster.ClusterScraper) *podEntityDTOBuilder {
	return &podEntityDTOBuilder{
		generalBuilder:       newGeneralBuilder(sink, cluster),
		stitchingManager:     stitchingManager,
		nodeNameUIDMap:       make(map[string]string),
		namespaceUIDMap:      make(map[string]string),
		podToVolumesMap:      make(map[string][]repository.MountedVolume),
		nodeNameToNodeMap:    make(map[string]*repository.KubeNode),
		mirrorPodToDaemonMap: make(map[string]bool),
		ClusterScraper:       clusterScraper,
	}
}

func (c *podEntityDTOBuilder) WithClusterKeyInjected(clusterKeyInjected string) *podEntityDTOBuilder {
	c.clusterKeyInjected = clusterKeyInjected
	return c
}

func (builder *podEntityDTOBuilder) WithNodeNameUIDMap(nodeNameUIDMap map[string]string) *podEntityDTOBuilder {
	builder.nodeNameUIDMap = nodeNameUIDMap
	return builder
}

func (builder *podEntityDTOBuilder) WithNameSpaceUIDMap(namespaceUIDMap map[string]string) *podEntityDTOBuilder {
	builder.namespaceUIDMap = namespaceUIDMap
	return builder
}

func (builder *podEntityDTOBuilder) WithPodToVolumesMap(podToVolumesMap map[string][]repository.MountedVolume) *podEntityDTOBuilder {
	builder.podToVolumesMap = podToVolumesMap
	return builder
}

func (builder *podEntityDTOBuilder) WithNodeNameToNodeMap(nodeNameNodeMap map[string]*repository.KubeNode) *podEntityDTOBuilder {
	builder.nodeNameToNodeMap = nodeNameNodeMap
	return builder
}

func (builder *podEntityDTOBuilder) WithRunningPods(runningPods []*api.Pod) *podEntityDTOBuilder {
	builder.runningPods = runningPods
	return builder
}

func (builder *podEntityDTOBuilder) WithPendingPods(pendingPods []*api.Pod) *podEntityDTOBuilder {
	builder.pendingPods = pendingPods
	return builder
}

func (builder *podEntityDTOBuilder) WithMirrorPodToDaemonMap(mirrorPodToDaemonMap map[string]bool) *podEntityDTOBuilder {
	builder.mirrorPodToDaemonMap = mirrorPodToDaemonMap
	return builder
}

func (builder *podEntityDTOBuilder) WithPodsWithLabelBasedAffinities(podsWithLabelBasedAffinities sets.String) *podEntityDTOBuilder {
	builder.podsWithLabelBasedAffinities = podsWithLabelBasedAffinities
	return builder
}

func (builder *podEntityDTOBuilder) WithHostnameSpreadPods(hostnameSpreadPods sets.String) *podEntityDTOBuilder {
	builder.hostnameSpreadPods = hostnameSpreadPods
	return builder
}

func (builder *podEntityDTOBuilder) WithOtherSpreadPods(otherSpreadPods sets.String) *podEntityDTOBuilder {
	builder.otherSpreadPods = otherSpreadPods
	return builder
}

func (builder *podEntityDTOBuilder) WithPodsToControllers(podsToControllers map[string]string) *podEntityDTOBuilder {
	builder.podsToControllers = podsToControllers
	return builder
}

func (builder *podEntityDTOBuilder) WithPodNonHostnameAntiTermTopologyKeys(podNonHostnameAntiTermTopologyKeys map[string]sets.String) *podEntityDTOBuilder {
	builder.podNonHostnameAntiTermTopologyKeys = podNonHostnameAntiTermTopologyKeys
	return builder
}

func (builder *podEntityDTOBuilder) WithNode2NodeGroups(node2nodegroups map[string]sets.String) *podEntityDTOBuilder {
	builder.node2nodegroups = node2nodegroups
	return builder
}

func (builder *podEntityDTOBuilder) WithAffinityMapper(mapper *podaffinity.AffinityMapper) *podEntityDTOBuilder {
	builder.affinityMapper = mapper
	return builder
}

func (builder *podEntityDTOBuilder) WithORMHandler(ormHandler resourcemapping.ORMHandler) *podEntityDTOBuilder {
	builder.ormHandler = ormHandler
	return builder
}

func (builder *podEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, []*proto.EntityDTO, []string, []string, []string) {
	glog.V(3).Infof("Building DTOs for running pods...")
	runningPodDTOs, runningPodsWithVolumes, runningMirrorPodUids, runningUnmovablePods := builder.buildDTOs(
		builder.runningPods, runningPodResCommTypeSold, runningPodResCommTypeBoughtFromNode)
	glog.V(3).Infof("Built %d running pod DTOs.", len(runningPodDTOs))
	glog.V(3).Infof("Building DTOs for pending pods...")
	pendingPodDTOs, pendingPodsWithVolumes, pendingMirrorPodUids, pendingUnmovablePods := builder.buildDTOs(
		builder.pendingPods, pendingPodResCommTypeSold, pendingPodResCommTypeBoughtFromNode)
	glog.V(3).Infof("Built %d pending pod DTOs.", len(pendingPodDTOs))
	podsWithVolumes := append(runningPodsWithVolumes, pendingPodsWithVolumes...)
	mirrorPodUids := append(runningMirrorPodUids, pendingMirrorPodUids...)
	unmovablePods := append(runningUnmovablePods, pendingUnmovablePods...)
	return runningPodDTOs, pendingPodDTOs, podsWithVolumes, mirrorPodUids, unmovablePods
}

// Helper method to retrieve the controller of the specified Pod
func (builder *podEntityDTOBuilder) getController(pod *api.Pod) (*repository.K8sController, bool) {
	parentInfo, err := util.GetPodParentInfo(pod)
	if err != nil || util.IsOwnerInfoEmpty(parentInfo) {
		return nil, false
	}
	controller, exists := builder.clusterSummary.ControllerMap[parentInfo.Uid]
	if exists && len(controller.OwnerReferences) > 0 && controllersWithParents.Has(controller.Kind) {
		ownerInfo, ownerExists := util.GetOwnerInfo(controller.OwnerReferences)
		if !ownerExists {
			return controller, ownerExists
		}
		controller, exists = builder.clusterSummary.ControllerMap[ownerInfo.Uid]
	}
	return controller, exists
}

// Helper method to determine if an operator-controlled Pod has an ORM that includes SLO
// scaling configuration
func (builder *podEntityDTOBuilder) hasSLOScaleORM(controller *repository.K8sController) bool {
	if builder.ormHandler == nil {
		return false
	}
	ownerInfo, isOwnerSet := util.GetOwnerInfo(controller.OwnerReferences)
	if !isOwnerSet {
		return false
	}
	if builder.ormHandler.HasORM(controller) {
		resourcePaths := []string{string(executor.ControllerHorizontalScalePathTemplate)}
		objMap, err := builder.ormHandler.CheckAndReplaceWithFirstClassOwners(ownerInfo.Unstructured(), resourcePaths)
		if err != nil {
			glog.Errorf("error in owner checking, err: %s", err.Error())
			return false
		}
		for obj, paths := range objMap {
			ownerResources, err := builder.ormHandler.GetOwnerResourcesForOwnedResources(obj, ownerInfo, paths)
			if len(ownerResources.OwnerResourcesMap) > 0 && err == nil {
				return true
			}
		}
	}
	return false
}

// Helper method to determine if a Pod is horizontally scalable. If not, provision/suspend
// actions will be disabled for the Pod.
func (builder *podEntityDTOBuilder) isHorizontallyScalable(pod *api.Pod) bool {
	turboPodName := pod.Namespace + "/" + pod.Name
	controller, exists := builder.getController(pod)
	if !exists {
		return false // can't scale without a controller
	}
	if controller.HasParent {
		if controller.HasSkipOperatorLabel() || controller.HasOperatorExclusion() {
			// users may include a label or configure exclusions that tell turbo to
			// generate and execute actions against the entities regardless of being
			// operator-controlled
			return true
		}
		if !horizontallScalableOperatorControlledControllerKinds.Has(controller.Kind) {
			glog.V(4).Infof("operator-controlled Pod %s provision/suspend disabled - controller Kind %s not supported",
				turboPodName, controller.Kind)
			return false
		}
		if !builder.hasSLOScaleORM(controller) {
			glog.V(4).Infof("operator-controlled Pod %s provision/suspend disabled - no ORM present",
				turboPodName)
			return false
		}
	}
	if !horizontallyScalableControllerKinds.Has(controller.Kind) {
		glog.V(4).Infof("Pod %s provision/suspend disabled - unsupported controller Kind %s",
			turboPodName, controller.Kind)
		return false
	}
	return true
}

// Build entityDTOs based on the given pod list.
func (builder *podEntityDTOBuilder) buildDTOs(pods []*api.Pod, resCommTypeSold,
	resCommTypeBoughtFromNode []metrics.ResourceType) ([]*proto.EntityDTO, []string, []string, []string) {
	var result []*proto.EntityDTO
	var podsWithVolumes []string
	var mirrorPodUids []string
	var unmovablePods []string
	for _, pod := range pods {
		// id.
		podID := string(pod.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podID)
		// determine if the pod is a daemon set pod as that determines the eligibility of the pod for different actions
		mirrorPodDaemon, hasKey := builder.mirrorPodToDaemonMap[podID]
		daemon := util.Daemon(pod) || mirrorPodDaemon
		if hasKey {
			mirrorPodUids = append(mirrorPodUids, podID)
		}

		// display name.
		displayName := util.GetPodClusterID(pod)
		entityDTOBuilder.DisplayName(displayName)

		// consumption resource commodities sold
		commoditiesSold, err := builder.getPodCommoditiesSold(pod, resCommTypeSold)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}
		// allocation resource commodities sold
		quotaCommoditiesSold, err := builder.getPodQuotaCommoditiesSold(pod)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}
		commoditiesSold = append(commoditiesSold, quotaCommoditiesSold...)
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought - from node provider
		commoditiesBoughtfromNode, err := builder.getPodCommoditiesBoughtFromNode(pod, resCommTypeBoughtFromNode)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}
		providerNodeUID, exist := builder.nodeNameUIDMap[pod.Spec.NodeName]
		if !exist {
			glog.Errorf("Error when create commoditiesBought for pod %s: Cannot find uuid for provider "+
				"node.", displayName)
			continue
		}
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, providerNodeUID)
		entityDTOBuilder = entityDTOBuilder.Provider(provider)

		// pods are movable across nodes except for the daemon pods
		if daemon {
			entityDTOBuilder.IsMovable(proto.EntityDTO_VIRTUAL_MACHINE, false)
		}

		entityDTOBuilder.BuysCommodities(commoditiesBoughtfromNode)

		// commodities bought - from node group provider
		commoditiesBoughtNodeGroup, err := builder.getPodCommoditiesBoughtFromNodeGroup(pod)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}

		// Set commodities bought from node group provider
		builder.SetCommoditiesBoughtFromNodeGroup(entityDTOBuilder, commoditiesBoughtNodeGroup, providerNodeUID)

		// If it is bare pod deployed without k8s controller, pod buys quota commodities directly from Namespace provider
		if !util.HasController(pod) {
			namespaceUID, exists := builder.namespaceUIDMap[pod.Namespace]
			if exists {
				commoditiesBoughtQuota, err := builder.getQuotaCommoditiesBought(namespaceUID, pod)
				if err != nil {
					glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
					continue
				}
				provider := sdkbuilder.CreateProvider(proto.EntityDTO_NAMESPACE, namespaceUID)
				entityDTOBuilder = entityDTOBuilder.Provider(provider)
				entityDTOBuilder.BuysCommodities(commoditiesBoughtQuota)
				// pods are not movable across namespaces
				entityDTOBuilder.IsMovable(proto.EntityDTO_NAMESPACE, false)
				// also set up the aggregatedBy relationship with the namespace
				entityDTOBuilder.AggregatedBy(namespaceUID)
			} else {
				glog.Errorf("Failed to get namespaceUID from namespace %s for pod %s", pod.Namespace, pod.Name)
			}
		} else {
			// If pod is deployed by k8s controller, pod buys quota commodities from WorkloadController provider
			controllerUID, err := util.GetControllerUID(pod, builder.metricsSink)
			if err != nil {
				glog.Errorf("Error when creating commoditiesBought for pod %s: %v", displayName, err)
				continue
			}
			commoditiesBoughtQuota, err := builder.getQuotaCommoditiesBought(controllerUID, pod)
			if err != nil {
				glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
				continue
			}
			provider := sdkbuilder.CreateProvider(proto.EntityDTO_WORKLOAD_CONTROLLER, controllerUID)
			entityDTOBuilder = entityDTOBuilder.Provider(provider)
			entityDTOBuilder.BuysCommodities(commoditiesBoughtQuota)
			// pods are not movable across WorkloadController
			entityDTOBuilder.IsMovable(proto.EntityDTO_WORKLOAD_CONTROLLER, false)
			// also set up the aggregatedBy relationship with the controller
			entityDTOBuilder.AggregatedBy(controllerUID)
		}

		mounts := builder.podToVolumesMap[displayName]
		controllable := util.Controllable(pod, mirrorPodDaemon)
		monitored := true
		suspendable := true
		provisionable := true
		powerState := proto.EntityDTO_POWERED_ON
		shopsTogether := len(commoditiesBoughtNodeGroup) != 0
		if !builder.isContainerMetricsAvailable(pod) && !utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		}
		// action eligibility for daemon pods
		if daemon {
			suspendable = false
			provisionable = false
			shopsTogether = false
		}
		if util.PodIsPending(pod) {
			mounts = nil
			controllable = false
			suspendable = false
			provisionable = false
			monitored = false
			shopsTogether = false
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		} else if !util.PodIsReady(pod) || util.PodIsTerminating(pod) {
			controllable = false
			suspendable = false
			provisionable = false
			monitored = false
			shopsTogether = false
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		}
		// Action eligibility for Job/CronJob pods
		if util.IsJob(pod) {
			controllable = false
			provisionable = false
			shopsTogether = false
			suspendable = false
			entityDTOBuilder.IsMovable(proto.EntityDTO_VIRTUAL_MACHINE, false)
		}
		if !builder.isHorizontallyScalable(pod) {
			// mark the pod as not provisiobale or suspendable so that SLO scale actions
			// are not generated
			provisionable = false
			suspendable = false
		}

		glog.V(4).Infof(
			"Pod %v: controllable: %v, suspendable: %v, provisionable: %v, monitored: %v, shopsTogether: %v, power state %v",
			displayName, controllable, suspendable, provisionable, monitored, shopsTogether, powerState)

		if utilfeature.DefaultFeatureGate.Enabled(features.PersistentVolumes) {
			// Commodities bought from volume mounts
			builder.buyCommoditiesFromVolumes(pod, mounts, entityDTOBuilder)
		}
		// entities' properties.
		properties, err := builder.getPodProperties(pod, mounts)
		if err != nil {
			glog.Errorf("Failed to get required pod properties: %s", err)
			continue
		}

		entityDto, err := entityDTOBuilder.
			WithProperties(properties).
			ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
				Controllable:  &controllable,
				Daemon:        &daemon,
				ShopsTogether: &shopsTogether,
			}).
			Monitored(monitored).
			ContainerPodData(builder.createContainerPodData(pod)).
			WithPowerState(powerState).
			IsProvisionable(provisionable).
			IsSuspendable(suspendable).
			Create()
		if err != nil {
			glog.Errorf("Failed to build Pod entityDTO: %s", err)
			continue
		}

		if len(mounts) > 0 {
			podsWithVolumes = append(podsWithVolumes, podID)
		}
		result = append(result, entityDto)

		if daemon {
			glog.V(4).Infof("Daemon pod DTO: %+v", entityDto)
		} else {
			glog.V(4).Infof("Pod DTO: %+v", entityDto)
		}

		if !builder.isMovable(pod) {
			unmovablePods = append(unmovablePods, podID)
		}
	}

	return result, podsWithVolumes, mirrorPodUids, unmovablePods
}

func (builder *podEntityDTOBuilder) isMovable(pod *api.Pod) bool {
	turboPodName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	ownerInfo, err := util.GetPodParentInfo(pod)
	if err != nil {
		glog.V(4).Infof("Pod %s unmovable: failed to get parent: %v", turboPodName, err)
		return false
	}
	if util.IsOwnerInfoEmpty(ownerInfo) {
		// bare pods are movable
		return true
	}
	if strings.EqualFold(ownerInfo.Kind, commonutil.KindVirtualMachineInstance) {
		// Movability for VirtualMachineInstance/KubeVirt pods is handled via its own
		// group and policy (KubeVirt Pods Move Recommend Only). Ignoring these pods
		// here so we don't have redundant policies disabling the moves.
		return true
	}
	controller, exists := builder.clusterSummary.ControllerMap[ownerInfo.Uid]
	if !exists {
		glog.V(4).Infof("Pod %s unmovable: no controller for owner: %s", turboPodName, ownerInfo.Uid)
		return false
	}
	if !movableControllerKinds.Has(controller.Kind) {
		glog.V(4).Infof("Pod %s unmovable: unsupported controller kind: %s", turboPodName, controller.Kind)
		return false
	}
	return true
}

func (builder *podEntityDTOBuilder) isContainerMetricsAvailable(pod *api.Pod) bool {
	// We consider that the metrics for a pods containers are available until we don't
	// explicitly see an availability metrics set to false.
	isAvailable := true
	entityKey := util.PodMetricIdAPI(pod)
	ownerMetricId := metrics.GenerateEntityStateMetricUID(metrics.PodType, entityKey, metrics.MetricsAvailability)
	availabilityMetric, err := builder.metricsSink.GetMetric(ownerMetricId)
	if err != nil {
		glog.Warningf("Error getting %s from metrics sink for pod %s --> %v", metrics.MetricsAvailability, entityKey, err)
	} else {
		availabilityMetricValue := availabilityMetric.GetValue()
		ok := false
		isAvailable, ok = availabilityMetricValue.(bool)
		if !ok {
			glog.Warningf("Error getting %s from metrics sink for pod %s. Wrong type: %T, Expected: bool.", metrics.MetricsAvailability, entityKey, availabilityMetricValue)
		}
	}
	return isAvailable
}

// VCPURequestQuota/VMemRequestQuota commodities.
// Build the CommodityDTOs sold  by the pod for vCPU, vMem and VMPMAcces.
// VMPMAccess is used to bind container to the hosting pod so the container is not moved out of the pod
func (builder *podEntityDTOBuilder) getPodCommoditiesSold(
	pod *api.Pod, resCommTypeSold []metrics.ResourceType) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO

	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(
		func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(false) }, resCommTypeSold...)

	// Resource Commodities
	podMId := util.PodMetricIdAPI(pod)
	resourceCommoditiesSold := builder.getResourceCommoditiesSold(metrics.PodType, podMId, resCommTypeSold, nil, attributeSetter)
	if len(resourceCommoditiesSold) != len(resCommTypeSold) {
		// Only return error when pod is ready. Unready pods may not have resource consumption, but we still want to
		// create the pod DTOs
		if util.PodIsReady(pod) {
			return nil, fmt.Errorf("mismatch num of sold commodities (%d Vs. %d) for pod %s",
				len(resourceCommoditiesSold), len(resCommTypeSold), pod.Name)
		}
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// vmpmAccess commodity
	podAccessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(string(pod.UID)).
		Capacity(accessCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, podAccessComm)

	return commoditiesSold, nil
}

// getPodQuotaCommoditiesSold builds the quota commodity DTOs sold by the pod
func (builder *podEntityDTOBuilder) getPodQuotaCommoditiesSold(pod *api.Pod) ([]*proto.CommodityDTO, error) {
	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(false) }, podQuotaCommType...)

	// Resource Commodities
	podMId := util.PodMetricIdAPI(pod)
	quotaCommoditiesSold := builder.getResourceCommoditiesSold(metrics.PodType, podMId, podQuotaCommType, nil, attributeSetter)
	if len(quotaCommoditiesSold) != len(podQuotaCommType) {
		return nil, fmt.Errorf("mismatch num of sold commidities (%d Vs. %d) for pod %s",
			len(quotaCommoditiesSold), len(podQuotaCommType), pod.Name)
	}
	return quotaCommoditiesSold, nil
}

// Build the CommodityDTOs bought by the pod from the node provider.
// Commodities bought are vCPU, vMem vmpm access, cluster
func (builder *podEntityDTOBuilder) getPodCommoditiesBoughtFromNode(pod *api.Pod,
	resCommTypeBoughtFromNode []metrics.ResourceType) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO

	// Resource Commodities.
	podMId := util.PodMetricIdAPI(pod)
	resourceCommoditiesBought := builder.getResourceCommoditiesBought(metrics.PodType, podMId, resCommTypeBoughtFromNode, nil, nil)
	if len(resourceCommoditiesBought) != len(resCommTypeBoughtFromNode) {
		// Only return error when pod is ready. Unready pods may not have resource consumption, but we still want to
		// create the pod DTOs
		if util.PodIsReady(pod) {
			return nil, fmt.Errorf("mismatch num of bought commidities from node (%d Vs. %d) for pod %s",
				len(resourceCommoditiesBought), len(resCommTypeBoughtFromNode), pod.Name)
		}
	}
	commoditiesBought = append(commoditiesBought, resourceCommoditiesBought...)

	// Label commodities
	for key, value := range pod.Spec.NodeSelector {
		selector := key + "=" + value
		labelComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_LABEL).
			Key(selector).
			Create()
		if err != nil {
			return nil, err
		}
		glog.V(5).Infof("Adding label commodity for Pod %s with key : %s", pod.Name, selector)
		commoditiesBought = append(commoditiesBought, labelComm)
	}

	// Label commodities
	// To honor the region/zone label on th node that the pod is running on if the pod has any PV attached
	if utilfeature.DefaultFeatureGate.Enabled(features.HonorAzLabelPvAffinity) {
		var anerr error
		commoditiesBought, anerr = builder.getRegionZoneLabelCommodity(pod, commoditiesBought)
		if anerr != nil {
			glog.Errorf("Failed to append the region/zone label commodity")
			return nil, anerr
		}
	}

	// Add pods parent qualified workload name as Label commodity to honor placement as per affinities
	// Nodes where this pod can be placed will sell this commodity
	// Also add segmentation commodity for pods which are part of hostname or topology spread workloads
	if utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) {
		affinityComms, err := builder.getAffinityLabelOrSegmentationCommoditiesFromNode(pod.Namespace + "/" + pod.Name)
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, affinityComms...)
	}

	// Cluster commodity.
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(metrics.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for current Kubernetes Cluster %s", metrics.Cluster, clusterInfo)
	} else {
		var clusterCommodityKey string
		var ok bool
		if len(strings.TrimSpace(builder.clusterKeyInjected)) != 0 {
			clusterCommodityKey = builder.clusterKeyInjected
			glog.V(4).Infof("Injecting cluster key for POD %s with key : %s", pod.Name, clusterCommodityKey)
		} else {
			clusterCommodityKey, ok = clusterInfo.GetValue().(string)
			if !ok {
				glog.Error("Failed to get cluster ID")
			} else {
				glog.V(4).Infof("adding cluster key for POD %s with key : %s", pod.Name, clusterCommodityKey)
			}
		}

		clusterComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
			Key(clusterCommodityKey).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, clusterComm)
	}

	if podMappings, exists := builder.affinityMapper.GetPodMappings(pod.Namespace + "/" + pod.Name); exists {
		for mapping := range podMappings {
			if mapping.Provider.ProviderType != podaffinity.NodeProvider {
				continue
			}
			commodityDTO, err := builder.buildAffinityCommodity(mapping.MappingKey)
			if err == nil {
				commoditiesBought = append(commoditiesBought, commodityDTO)
			}
		}
	}

	return commoditiesBought, nil
}

func (builder *podEntityDTOBuilder) buildAffinityCommodity(mappingKey podaffinity.MappingKey) (*proto.CommodityDTO, error) {
	var commodity *proto.CommodityDTO
	var err error
	switch mappingKey.MappingType {
	case podaffinity.AffinitySrc:
		commodity, err = sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY).
			Key(mappingKey.CommodityKey).
			Used(podaffinity.NONE).
			Create()
	case podaffinity.AffinityDst:
		commodity, err = sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_AFFINITY).
			Key(mappingKey.CommodityKey).
			Used(1).
			Create()
	case podaffinity.AntiAffinitySrc:
		commodity, err = sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY).
			Key(mappingKey.CommodityKey).
			Peak(podaffinity.NONE).
			Used(1).
			Create()
	case podaffinity.AntiAffinityDst:
		commodity, err = sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY).
			Key(mappingKey.CommodityKey).
			Peak(1).
			Used(podaffinity.NONE).
			Create()
	}

	if err != nil {
		glog.Errorf("Failed to ceate %s commodity for mapping %s", commodity, mappingKey.CommodityKey)
		return nil, err
	}

	return commodity, err
}

// Build the CommodityDTOs bought by the pod from the node provider.
// Commodities bought are affinity commodities (segmentaion commodities for now)
func (builder *podEntityDTOBuilder) getPodCommoditiesBoughtFromNodeGroup(pod *api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO

	// Check if the feature gate for new affinity processing is enabled.
	if utilfeature.DefaultFeatureGate.Enabled(features.NewAffinityProcessing) {
		podName := pod.Namespace + "/" + pod.Name
		// Call the getAffinityRelatedCommodities method with isFromNode flag set to false indication getting affinity commodities from NodeGroup
		affinityComms, err := builder.getSegmentationCommoditiesBoughtFromNodegroup(pod.Namespace + "/" + pod.Name)
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, affinityComms...)

		if utilfeature.DefaultFeatureGate.Enabled(features.PeerToPeerAffinityAntiaffinity) {

			podMappings, exists := builder.affinityMapper.GetPodMappings(podName)
			if !exists {
				return commoditiesBought, nil
			}

			for mapping := range podMappings {
				if mapping.Provider.ProviderType != podaffinity.NodeGroupProvider {
					continue
				}
				commodityDTO, err := builder.buildAffinityCommodity(mapping.MappingKey)
				if err == nil {
					commoditiesBought = append(commoditiesBought, commodityDTO)
				}
			}

		}
	}
	return commoditiesBought, nil

}

func (builder *podEntityDTOBuilder) getAffinityLabelOrSegmentationCommoditiesFromNode(podQualifiedName string) ([]*proto.CommodityDTO, error) {
	var affinityComms []*proto.CommodityDTO = nil
	if builder.podsWithLabelBasedAffinities.Has(podQualifiedName) {
		// This is for everything else (not using peer to peer affinities) when PeerToPeerAffinityAntiaffinity=true,
		// eg nodeaffinities, volume affinities, etc
		// and additionally for workloads that are not spread workloads when PeerToPeerAffinityAntiaffinity=false.
		// podsWithLabelBasedAffinities is filled accordingly (based on featureflags) while processing affinities
		// in affinityProcessor
		key, exists := builder.podsToControllers[podQualifiedName]
		if !exists { // A pod can be a standalone pod also
			key = podQualifiedName
		}
		comm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_LABEL).
			Key(key).
			Create()
		if err != nil {
			glog.Errorf("Failed to ceate affinity label commodity for pod %s", podQualifiedName)
			return nil, err
		}
		glog.V(5).Infof("Adding affinity label commodity for Pod %s", podQualifiedName)
		affinityComms = append(affinityComms, comm)
	}

	// hostnameSpreadPods is not dependent on a featureflag, this was released without a feature flag
	// TODO: Question for reviewers, should we correct this?
	// Correction would be to put this also behind SegmentationBasedTopologySpread
	if builder.hostnameSpreadPods.Has(podQualifiedName) {
		key, exists := builder.podsToControllers[podQualifiedName]
		if !exists {
			// spread workload means that this pod should have a parent
			// as we could not find its parent, there is no point adding this commodity for this pod
			glog.Warningf("Parent for spread workload pod %s not found", podQualifiedName)
			return affinityComms, nil
		}

		comm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
			Key(key).
			Used(1).
			Create()
		if err != nil {
			glog.Errorf("Failed to ceate hostname Spread based affinity segmentation commodity for pod %s", podQualifiedName)
			return nil, err
		}
		glog.V(5).Infof("Adding affinity hostname Spread based affinity segmentation commodity for Pod %s with key %s", podQualifiedName, key)
		affinityComms = append(affinityComms, comm)

		return affinityComms, nil
	}

	return affinityComms, nil
}

func (builder *podEntityDTOBuilder) getSegmentationCommoditiesBoughtFromNodegroup(podQualifiedName string) ([]*proto.CommodityDTO, error) {
	var affinityComms []*proto.CommodityDTO = nil
	segmentationBasedSpread := utilfeature.DefaultFeatureGate.Enabled(features.SegmentationBasedTopologySpread)
	if segmentationBasedSpread && !builder.otherSpreadPods.Has(podQualifiedName) {
		// this pod does not figure in other spread workloads, we do not add any SEGMENTATION commodity wrt nodegroups
		// the label commodity would rather be added via getAffinityRelatedCommoditiesFromNode() and nodes will sell it
		return affinityComms, nil
	}

	key, exists := builder.podsToControllers[podQualifiedName]
	if !exists {
		// spread workload means that this pod should have a parent
		// as we could not find its parent, there is no point adding this commodity for this pod
		glog.Warningf("Parent for spread workload pod %s not found", podQualifiedName)
		return affinityComms, nil
	}

	tpKeys, exists := builder.podNonHostnameAntiTermTopologyKeys[podQualifiedName]
	if !exists {
		return affinityComms, nil
	}
	for tpKey := range tpKeys {
		comkey := tpKey + "@" + key
		comm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_SEGMENTATION).
			Key(comkey).
			Used(1).Create()

		if err != nil {
			glog.Errorf("Failed to ceate affinity segmentation commodity for pod %s", podQualifiedName)
			return nil, err
		}
		glog.V(5).Infof("Adding othere spread based affinity segmentation commodity for Pod %s with key %s", podQualifiedName, comkey)
		affinityComms = append(affinityComms, comm)
	}

	return affinityComms, nil
}

// Build the CommodityDTOs bought by the pod from the quota provider.
func (builder *podEntityDTOBuilder) getQuotaCommoditiesBought(providerUID string, pod *api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// Resource Commodities.
	for _, resourceType := range podQuotaCommType {
		commBought, err := builder.getResourceCommodityBoughtWithKey(metrics.PodType, key,
			resourceType, providerUID, nil, nil)
		if err != nil {
			glog.Warningf("Cannot build %s bought by pod %s from quota provider %s: %v",
				resourceType, key, providerUID, err)
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}
	return commoditiesBought, nil
}

// Build the CommodityDTOs bought by the pod from the Volumes.
func (builder *podEntityDTOBuilder) buyCommoditiesFromVolumes(pod *api.Pod, mounts []repository.MountedVolume, dtoBuilder *sdkbuilder.EntityDTOBuilder) {
	podKey := util.PodKeyFunc(pod)

	for _, mount := range mounts {
		mountName := mount.MountName
		volEntityID := util.PodVolumeMetricId(podKey, mountName)
		commBought, err := builder.getResourceCommodityBoughtWithKey(metrics.PodType, volEntityID,
			metrics.StorageAmount, "", nil, nil)
		if err != nil {
			glog.Warningf("Cannot build commodity %s bought by pod %s mounted %s from volume %s: %v",
				metrics.StorageAmount, podKey, mountName, mount.UsedVolume.Name, err)
			continue
		}

		if mount.UsedVolume == nil {
			glog.Errorf("Error when create commoditiesBought for pod %s mounting %s: Cannot find uuid for provider "+
				"Vol: ", podKey, mountName)
			continue
		}

		providerVolUID := string(mount.UsedVolume.UID)

		provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_VOLUME, providerVolUID)
		dtoBuilder = dtoBuilder.Provider(provider)
		dtoBuilder.IsMovable(proto.EntityDTO_VIRTUAL_VOLUME, false).
			IsStartable(proto.EntityDTO_VIRTUAL_VOLUME, false).
			IsScalable(proto.EntityDTO_VIRTUAL_VOLUME, false)

		// Each pod mounts any given volume only once
		dtoBuilder.BuysCommodities([]*proto.CommodityDTO{commBought})
	}
}

// Get the properties of the pod. This includes property related to pod cluster property.
func (builder *podEntityDTOBuilder) getPodProperties(pod *api.Pod, vols []repository.MountedVolume) ([]*proto.EntityDTO_EntityProperty, error) {
	var properties []*proto.EntityDTO_EntityProperty
	// additional node cluster info property.
	podProperties := property.BuildPodProperties(pod, builder.clusterSummary.Name)
	properties = append(properties, podProperties...)

	podClusterID := util.GetPodClusterID(pod)
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, fmt.Errorf("cannot find the hosting node ID for pod %s", podClusterID)
	}

	stitchingProperty, err := builder.stitchingManager.BuildDTOProperty(nodeName, false)
	if err != nil {
		if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
			// dont error out
			return nil, nil
		}
		return nil, fmt.Errorf("failed to build EntityDTO for Pod %s: %s", podClusterID, err)
	}
	properties = append(properties, stitchingProperty)

	if len(vols) > 0 {
		var apiVols []*api.PersistentVolume
		for _, vol := range vols {
			apiVols = append(apiVols, vol.UsedVolume)
		}

		m := stitching.NewVolumeStitchingManager()
		err := m.ProcessVolumes(apiVols)
		if err == nil {
			properties = append(properties, m.BuildDTOProperties(false)...)
		}
		// Add volume property if the pod has volumes attached
		properties = property.AddVolumeProperties(properties)
	}

	// Add VirtualMachineInstance
	ownerInfo, err := util.GetPodParentInfo(pod)
	if err == nil {
		if ownerInfo.Kind == commonutil.KindVirtualMachineInstance {
			properties = property.AddVirtualMachineInstanceProperties(properties, "true")
		} else {
			properties = property.AddVirtualMachineInstanceProperties(properties, "false")
		}
	}

	return properties, nil
}

func (builder *podEntityDTOBuilder) createContainerPodData(pod *api.Pod) *proto.EntityDTO_ContainerPodData {
	fullName := pod.Name
	ns := pod.Namespace
	nodeCPUFrequency, err := builder.getNodeCPUFrequencyViaPod(pod)
	if err != nil {
		glog.Warningf("Could not get node cpu frequency for pod[%s/%s]."+
			"\nHosted application usage data may not reflect right Mhz values: %v", ns, fullName, err)
	}
	podData := &proto.EntityDTO_ContainerPodData{
		HostingNodeCpuFrequency: &nodeCPUFrequency,
	}

	// Add IP address in ContainerPodData. Some pods (system pods and daemonset pods) may use the host IP as the pod IP,
	// in which case the IP address will not be unique (in the k8s cluster) and hence not populated in ContainerPodData.
	port := "not-set"
	if pod.Status.PodIP != "" && pod.Status.PodIP != pod.Status.HostIP {
		// Note the port needs to be set if needed
		podData.IpAddress = &(pod.Status.PodIP)
		podData.Port = &port
		podData.FullName = &fullName
		podData.Namespace = &ns
	}

	if util.PodIsReady(pod) && pod.Status.PodIP == "" {
		glog.Errorf("No IP found for pod %s", fullName)
	}

	if builder.ClusterScraper != nil {
		ownerInfo, _, _, err := builder.ClusterScraper.GetPodControllerInfo(pod, true)
		if err != nil || util.IsOwnerInfoEmpty(ownerInfo) {
			// The pod does not have a controller
			glog.V(3).Infof("Skip adding workload controller data for pod %v/%v: pod has no controller.",
				pod.Namespace, pod.Name)
			return podData
		}

		controllerData := CreateWorkloadControllerDataByControllerType(ownerInfo.Kind)
		podData.ControllerData = controllerData
	} else {
		// ClusterScraper is not initialized
		glog.V(3).Infof("Skip adding workload controller data for pod %v/%v: ClusterScraper is not initialized.",
			pod.Namespace, pod.Name)
	}

	return podData
}

func (builder *podEntityDTOBuilder) getRegionZoneLabelCommodity(pod *api.Pod, commoditiesBought []*proto.CommodityDTO) ([]*proto.CommodityDTO, error) {
	if pod == nil {
		return nil, fmt.Errorf("the pod's pointer is nil")
	}
	var err error
	displayName := util.GetPodClusterID(pod)
	mounts := builder.podToVolumesMap[displayName]
	if len(mounts) > 0 {
		if node, ok := builder.nodeNameToNodeMap[pod.Spec.NodeName]; ok {
			if value, isFound := node.Labels[regionLabelName]; isFound {
				selector := regionLabelName + "=" + value
				commoditiesBought, err = AppendNewLabelCommodityToList(commoditiesBought, selector)
				if err != nil {
					return nil, err
				}
				glog.V(4).Infof("Added label commodity for Pod %s with key : %s", pod.Name, selector)
			}
			if value, isFound := node.Labels[zoneLabelName]; isFound {
				selector := zoneLabelName + "=" + value
				commoditiesBought, err = AppendNewLabelCommodityToList(commoditiesBought, selector)
				if err != nil {
					return nil, err
				}
				glog.V(4).Infof("Added label commodity for Pod %s with key : %s", pod.Name, selector)
			}
		}
	}
	return commoditiesBought, nil
}

func (builder *podEntityDTOBuilder) SetCommoditiesBoughtFromNodeGroup(entityDTOBuilder *sdkbuilder.EntityDTOBuilder, commoditiesBoughtNodeGroup []*proto.CommodityDTO, nodeId string) {
	ngId2segcommodities := make(map[string][]*proto.CommodityDTO) // Map of nodeid ----> commodities
	for _, commodity := range commoditiesBoughtNodeGroup {
		keyparts := strings.Split(commodity.GetKey(), "@")
		if len(keyparts) > 0 {
			tpkey := keyparts[0]
			if strings.Contains(tpkey, "|") {
				tpkey = strings.Split(tpkey, "|")[0]
			}
			for nodeGroupId := range builder.node2nodegroups[nodeId] {
				ngparts := strings.Split(nodeGroupId, "=")
				if len(ngparts) > 0 && ngparts[0] == tpkey {
					commoditiesLst := ngId2segcommodities[nodeGroupId]
					commoditiesLst = append(commoditiesLst, commodity)
					ngId2segcommodities[nodeGroupId] = commoditiesLst
					break
				}
			}
		}
	}
	for ngid, commoditiesLst := range ngId2segcommodities {
		ngProvider := sdkbuilder.CreateProvider(proto.EntityDTO_NODE_GROUP, ngid)
		entityDTOBuilder = entityDTOBuilder.Provider(ngProvider)
		entityDTOBuilder.BuysCommodities(commoditiesLst)
	}
}

func AppendNewLabelCommodityToList(commoditiesList []*proto.CommodityDTO, key string) ([]*proto.CommodityDTO, error) {
	labelComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_LABEL).
		Key(key).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesList = append(commoditiesList, labelComm)
	return commoditiesList, nil
}
