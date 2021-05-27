package dtofactory

import (
	"fmt"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	v1 "k8s.io/api/core/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/golang/glog"
)

const (
	applicationCommodityDefaultCapacity = 1e10
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
)

type podEntityDTOBuilder struct {
	generalBuilder
	stitchingManager *stitching.StitchingManager
	nodeNameUIDMap   map[string]string
	namespaceUIDMap  map[string]string
	podToVolumesMap  map[string][]repository.MountedVolume
	runningPods      []*api.Pod
	pendingPods      []*api.Pod
}

func NewPodEntityDTOBuilder(sink *metrics.EntityMetricSink, stitchingManager *stitching.StitchingManager) *podEntityDTOBuilder {
	return &podEntityDTOBuilder{
		generalBuilder:   newGeneralBuilder(sink),
		stitchingManager: stitchingManager,
		nodeNameUIDMap:   make(map[string]string),
		namespaceUIDMap:  make(map[string]string),
		podToVolumesMap:  make(map[string][]repository.MountedVolume),
	}
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

func (builder *podEntityDTOBuilder) WithRunningPods(runningPods []*api.Pod) *podEntityDTOBuilder {
	builder.runningPods = runningPods
	return builder
}

func (builder *podEntityDTOBuilder) WithPendingPods(pendingPods []*api.Pod) *podEntityDTOBuilder {
	builder.pendingPods = pendingPods
	return builder
}

func (builder *podEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, []*proto.EntityDTO) {
	glog.V(3).Infof("Building DTOs for running pods...")
	runningPodDTOs := builder.buildDTOs(
		builder.runningPods, runningPodResCommTypeSold, runningPodResCommTypeBoughtFromNode)
	glog.V(3).Infof("Built %d running pod DTOs.", len(runningPodDTOs))
	glog.V(3).Infof("Building DTOs for pending pods...")
	pendingPodDTOs := builder.buildDTOs(
		builder.pendingPods, pendingPodResCommTypeSold, pendingPodResCommTypeBoughtFromNode)
	glog.V(3).Infof("Built %d pending pod DTOs.", len(pendingPodDTOs))
	return runningPodDTOs, pendingPodDTOs
}

// Build entityDTOs based on the given pod list.
func (builder *podEntityDTOBuilder) buildDTOs(pods []*api.Pod, resCommTypeSold,
	resCommTypeBoughtFromNode []metrics.ResourceType) []*proto.EntityDTO {
	var result []*proto.EntityDTO

	for _, pod := range pods {
		// id.
		podID := string(pod.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podID)

		// determine if the pod is a daemon set pod as that determines the eligibility of the pod for different actions
		daemon := util.Daemon(pod)

		// display name.
		displayName := util.GetPodClusterID(pod)
		entityDTOBuilder.DisplayName(displayName)
		cpuFrequency, err := builder.getNodeCPUFrequency(util.NodeKeyFromPodFunc(pod))
		if err != nil {
			glog.Errorf("Failed to build EntityDTO for pod %s: %v", displayName, err)
			continue
		}
		// consumption resource commodities sold
		commoditiesSold, err := builder.getPodCommoditiesSold(pod, cpuFrequency, resCommTypeSold)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}
		// allocation resource commodities sold
		quotaCommoditiesSold, err := builder.getPodQuotaCommoditiesSold(pod, cpuFrequency)
		if err != nil {
			glog.Warningf("Skip building DTO for pod %s: %s", displayName, err)
			continue
		}
		commoditiesSold = append(commoditiesSold, quotaCommoditiesSold...)
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought - from node provider
		commoditiesBought, err := builder.getPodCommoditiesBought(pod, cpuFrequency, resCommTypeBoughtFromNode)
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

		entityDTOBuilder.BuysCommodities(commoditiesBought)

		// If it is bare pod deployed without k8s controller, pod buys quota commodities directly from Namespace provider
		if !util.HasController(pod) {
			namespaceUID, exists := builder.namespaceUIDMap[pod.Namespace]
			if exists {
				commoditiesBoughtQuota, err := builder.getQuotaCommoditiesBought(namespaceUID, pod, cpuFrequency)
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
			commoditiesBoughtQuota, err := builder.getQuotaCommoditiesBought(controllerUID, pod, cpuFrequency)
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
		controllable := util.Controllable(pod)
		monitored := true
		suspendable := true
		provisionable := true
		powerState := proto.EntityDTO_POWERED_ON
		// action eligibility for daemon pods
		if daemon {
			suspendable = false
			provisionable = false
		}
		if util.PodIsPending(pod) {
			mounts = nil
			controllable = false
			suspendable = false
			provisionable = false
			monitored = false
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		} else if !util.PodIsReady(pod) {
			controllable = false
			suspendable = false
			provisionable = false
			monitored = false
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		}
		glog.V(4).Infof(
			"Pod %v: controllable: %v, suspendable: %v, provisionable: %v, monitored: %v, power state %v",
			displayName, controllable, suspendable, provisionable, monitored, powerState)

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
				Controllable: &controllable,
				Daemon:       &daemon,
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

		result = append(result, entityDto)

		if daemon {
			glog.V(4).Infof("Daemon pod DTO: %+v", entityDto)
		} else {
			glog.V(4).Infof("Pod DTO: %+v", entityDto)
		}
	}

	return result
}

// VCPURequestQuota/VMemRequestQuota commodities.
// Build the CommodityDTOs sold  by the pod for vCPU, vMem and VMPMAcces.
// VMPMAccess is used to bind container to the hosting pod so the container is not moved out of the pod
func (builder *podEntityDTOBuilder) getPodCommoditiesSold(
	pod *api.Pod, cpuFrequency float64, resCommTypeSold []metrics.ResourceType) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO

	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().
		Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPURequest)

	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(
		func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(false) }, resCommTypeSold...)

	// Resource Commodities
	podMId := util.PodMetricIdAPI(pod)
	resourceCommoditiesSold := builder.getResourceCommoditiesSold(metrics.PodType, podMId, resCommTypeSold, converter, attributeSetter)
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
func (builder *podEntityDTOBuilder) getPodQuotaCommoditiesSold(pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	// CPU resource needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 {
		// If CPU resource quota capacity is repository.DEFAULT_METRIC_CAPACITY_VALUE (infinity), skip converting from
		// cores to frequency.
		if input == repository.DEFAULT_METRIC_CAPACITY_VALUE {
			return input
		}
		return input * cpuFrequency
	}, metrics.CPULimitQuota, metrics.CPURequestQuota)

	attributeSetter := NewCommodityAttrSetter()
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(false) }, podQuotaCommType...)

	// Resource Commodities
	podMId := util.PodMetricIdAPI(pod)
	quotaCommoditiesSold := builder.getResourceCommoditiesSold(metrics.PodType, podMId, podQuotaCommType, converter, attributeSetter)
	if len(quotaCommoditiesSold) != len(podQuotaCommType) {
		return nil, fmt.Errorf("mismatch num of sold commidities (%d Vs. %d) for pod %s",
			len(quotaCommoditiesSold), len(podQuotaCommType), pod.Name)
	}
	return quotaCommoditiesSold, nil
}

// Build the CommodityDTOs bought by the pod from the node provider.
// Commodities bought are vCPU, vMem vmpm access, cluster
func (builder *podEntityDTOBuilder) getPodCommoditiesBought(
	pod *api.Pod, cpuFrequency float64, resCommTypeBoughtFromNode []metrics.ResourceType) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO

	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU, metrics.CPURequest)

	// Resource Commodities.
	podMId := util.PodMetricIdAPI(pod)
	resourceCommoditiesBought := builder.getResourceCommoditiesBought(metrics.PodType, podMId, resCommTypeBoughtFromNode, converter, nil)
	if len(resourceCommoditiesBought) != len(resCommTypeBoughtFromNode) {
		// Only return error when pod is ready. Unready pods may not have resource consumption, but we still want to
		// create the pod DTOs
		if util.PodIsReady(pod) {
			return nil, fmt.Errorf("mismatch num of bought commidities from node (%d Vs. %d) for pod %s",
				len(resourceCommoditiesBought), len(resCommTypeBoughtFromNode), pod.Name)
		}
	}
	commoditiesBought = append(commoditiesBought, resourceCommoditiesBought...)

	// Access commodities: selectors.
	for key, value := range pod.Spec.NodeSelector {
		selector := key + "=" + value
		accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
			Key(selector).
			Create()
		if err != nil {
			return nil, err
		}
		glog.V(5).Infof("Adding access commodity for Pod %s with key : %s", pod.Name, selector)
		commoditiesBought = append(commoditiesBought, accessComm)
	}

	// Cluster commodity.
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(metrics.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for current Kubernetes Cluster %s", metrics.Cluster, clusterInfo)
	} else {
		clusterCommodityKey, ok := clusterInfo.GetValue().(string)
		if !ok {
			glog.Error("Failed to get cluster ID")
		}
		clusterComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
			Key(clusterCommodityKey).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBought = append(commoditiesBought, clusterComm)
	}

	return commoditiesBought, nil
}

// Build the CommodityDTOs bought by the pod from the quota provider.
func (builder *podEntityDTOBuilder) getQuotaCommoditiesBought(providerUID string, pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	key := util.PodKeyFunc(pod)

	// cpu allocation needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 {
		return input * cpuFrequency
	}, metrics.CPULimitQuota, metrics.CPURequestQuota)

	// Resource Commodities.
	for _, resourceType := range podQuotaCommType {
		commBought, err := builder.getResourceCommodityBoughtWithKey(metrics.PodType, key,
			resourceType, providerUID, converter, nil)
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
	podProperties := property.BuildPodProperties(pod)
	properties = append(properties, podProperties...)

	podClusterID := util.GetPodClusterID(pod)
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, fmt.Errorf("cannot find the hosting node ID for pod %s", podClusterID)
	}

	stitchingProperty, err := builder.stitchingManager.BuildDTOProperty(nodeName, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build EntityDTO for Pod %s: %s", podClusterID, err)
	}
	properties = append(properties, stitchingProperty)

	if len(vols) > 0 {
		var apiVols []*v1.PersistentVolume
		for _, vol := range vols {
			apiVols = append(apiVols, vol.UsedVolume)
		}

		m := stitching.NewVolumeStitchingManager()
		err := m.ProcessVolumes(apiVols)
		if err == nil {
			properties = append(properties, m.BuildDTOProperties(false)...)
		}
	}

	return properties, nil
}

func (builder *podEntityDTOBuilder) createContainerPodData(pod *api.Pod) *proto.EntityDTO_ContainerPodData {
	// Add IP address in ContainerPodData. Some pods (system pods and daemonset pods) may use the host IP as the pod IP,
	// in which case the IP address will not be unique (in the k8s cluster) and hence not populated in ContainerPodData.
	fullName := pod.Name
	ns := pod.Namespace
	port := "not-set"
	if pod.Status.PodIP != "" && pod.Status.PodIP != pod.Status.HostIP {
		return &proto.EntityDTO_ContainerPodData{
			// Note the port needs to be set if needed
			IpAddress: &(pod.Status.PodIP),
			Port:      &port,
			FullName:  &fullName,
			Namespace: &ns,
		}
	}

	if util.PodIsReady(pod) && pod.Status.PodIP == "" {
		glog.Errorf("No IP found for pod %s", fullName)
	}
	return nil
}
