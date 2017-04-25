package probe

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "github.com/turbonomic/kubeturbo/pkg/cadvisor"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe/stitching"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

var container2PodMap map[string]string = make(map[string]string)

var podIP2PodMap map[string]*api.Pod = make(map[string]*api.Pod)

// This map keep records of pods whose states are set to inactive in VMT server side. The key is the pod identifier (podNamespace/podName)
var inactivePods map[string]struct{} = make(map[string]struct{})

// TODO, this is a quick fix
var podResourceConsumptionMap map[string]*PodResourceStat
var nodePodMap map[string][]string
var podNodeMap map[string]string
var podAppTypeMap map[string]string

// Pods Getter is such func that gets all the pods match the provided namespace, labels and fiels.
type PodsGetter func(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error)

type PodProbe struct {
	podGetter        PodsGetter
	stitchingManager *stitching.StitchingManager
}

func NewPodProbe(getter PodsGetter, config *ProbeConfig, stitchingManager *stitching.StitchingManager) *PodProbe {
	inactivePods = make(map[string]struct{})
	podResourceConsumptionMap = make(map[string]*PodResourceStat)
	nodePodMap = make(map[string][]string)
	podNodeMap = make(map[string]string)
	podAppTypeMap = make(map[string]string)

	return &PodProbe{
		podGetter:        getter,
		stitchingManager: stitchingManager,
	}
}

func (this *PodProbe) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	if this.podGetter == nil {
		return nil, fmt.Errorf("Error. podGetter is not set.")
	}

	return this.podGetter(namespace, label, field)
}

type VMTPodGetter struct {
	kubeClient *client.Client
}

func NewVMTPodGetter(kubeClient *client.Client) *VMTPodGetter {
	return &VMTPodGetter{
		kubeClient: kubeClient,
	}
}

// Get pods match specified namespace, label and field.
func (this *VMTPodGetter) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	listOption := &api.ListOptions{
		LabelSelector: label,
		FieldSelector: field,
	}
	podList, err := this.kubeClient.Pods(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error getting all the desired pods from Kubernetes cluster: %s", err)
	}
	var podItems []*api.Pod
	for _, pod := range podList.Items {
		p := pod
		if pod.Status.Phase != api.PodRunning {
			// Skip pods those are not running.
			continue
		}
		hostIP := p.Status.PodIP
		podIP2PodMap[hostIP] = &p
		podItems = append(podItems, &p)
	}
	glog.V(2).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems, nil
}

// Get pod resource usage from Kubernetes cluster and return a list of entityDTOs.
func (podProbe *PodProbe) parsePodFromK8s(pods []*api.Pod) (result []*proto.EntityDTO, err error) {
	podContainers, err := podProbe.groupContainerByPod()
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {

		podResourceStat, err := podProbe.getPodResourceStat(pod, podContainers)
		if err != nil {
			glog.Warningf("Cannot get resource consumption for pod %s: %s", pod.Namespace+"/"+pod.Name, err)
			continue
		}

		commoditiesSold, err := podProbe.getCommoditiesSold(pod, podResourceStat)
		if err != nil {
			return nil, err
		}
		commoditiesBought, err := podProbe.getCommoditiesBought(pod, podResourceStat)
		if err != nil {
			return nil, err
		}

		entityDto, _ := podProbe.buildPodEntityDTO(pod, commoditiesSold, commoditiesBought)

		result = append(result, entityDto)
	}

	return
}

// Group containers according to the hosting pod.
// TODO. Here we also create a container2PodMap, used by processProbe.
func (podProbe *PodProbe) groupContainerByPod() (map[string][]*vmtAdvisor.Container, error) {
	podContainers := make(map[string][]*vmtAdvisor.Container)

	for _, host := range hostSet {
		// use cadvisor to get all containers on that host
		cadvisor := &vmtAdvisor.CadvisorSource{}

		// Only get subcontainers.
		subcontainers, _, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
		if err != nil {
			glog.Errorf("Error when grouping containers according to pod. %s", err)
			continue
		}

		// Map container to each pod. Key is pod name, value is container.
		for _, container := range subcontainers {
			spec := container.Spec
			if &spec != nil && container.Spec.Labels != nil {
				// TODO! hardcoded here. Works but not good. Maybe can find better solution?
				// The value returned here is namespace/podname
				if podName, ok := container.Spec.Labels["io.kubernetes.pod.name"]; ok {
					if podNamespace, hasNamespace := container.Spec.Labels["io.kubernetes.pod.namespace"]; hasNamespace {
						podName = podNamespace + "/" + podName
					}
					glog.V(4).Infof("Container %s is in Pod %s", container.Name, podName)
					var containers []*vmtAdvisor.Container
					if ctns, exist := podContainers[podName]; exist {
						containers = ctns
					}
					containers = append(containers, container)
					podContainers[podName] = containers

					// Map container to hosting pod. This map will be used in application probe.
					container2PodMap[container.Name] = podName
				}
			}
		}
	}

	return podContainers, nil
}

// Get the current pod resource capacity and usage.
func (podProbe *PodProbe) getPodResourceStat(pod *api.Pod, podContainers map[string][]*vmtAdvisor.Container) (*PodResourceStat, error) {
	podNameWithNamespace := pod.Namespace + "/" + pod.Name
	// get cpu frequency
	machineInfo, ok := nodeMachineInfoMap[pod.Spec.NodeName]
	if !ok {
		return nil, fmt.Errorf("Cannot get machine information for %s", pod.Spec.NodeName)
	}
	cpuFrequency := machineInfo.CpuFrequency / 1000

	cpuCapacity, memCapacity, err := GetResourceLimits(pod)
	if err != nil {
		return nil, err
	}
	if cpuCapacity == 0 {
		cpuCapacity = float64(machineInfo.NumCores)
	} else {
		glog.V(4).Infof("Get cpu limit for Pod %s, is %f", pod.Name, cpuCapacity)
	}
	if memCapacity == 0 {
		memCapacity = float64(machineInfo.MemoryCapacity) / 1024
	}
	glog.V(4).Infof("Cpu cap of Pod %s in k8s format is %f", pod.Name, cpuCapacity)

	// the cpu return value is in KHz. VMTurbo uses MHz. So here should divide 1000
	podCpuCapacity := cpuCapacity * float64(cpuFrequency)
	podMemCapacity := memCapacity // Mem is in bytes, convert to Kb
	glog.V(4).Infof("Cpu cap of Pod %s is %f", pod.Name, podCpuCapacity)
	glog.V(4).Infof("Mem cap of Pod %s is %f", pod.Name, podMemCapacity)

	podCpuUsed := float64(0)
	podMemUsed := float64(0)

	if containers, ok := podContainers[podNameWithNamespace]; ok {
		for _, container := range containers {
			containerStats := container.Stats
			// We need at least two data points.
			if len(containerStats) < 2 {
				//TODO, maybe a warning is enough?
				glog.Warningf("Not enough data for %s. Skip.", podNameWithNamespace)
				continue
			}
			// Get the average of all the available data.
			dataSampleNumber := len(containerStats)
			glog.V(4).Infof("data sample number is %d", dataSampleNumber)

			currentStat := containerStats[len(containerStats)-1]
			prevStat := containerStats[0]
			rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
			intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
			glog.V(4).Infof("%d - %d = rawUsage is %d nanocore.", currentStat.Cpu.Usage.Total,
				prevStat.Cpu.Usage.Total, rawUsage)
			glog.V(4).Infof("intervalInNs is %d ns", intervalInNs)

			podCpuUsed += float64(rawUsage) / float64(intervalInNs)

			// memory
			var currContainerMemUsed float64
			for i := 1; i <= dataSampleNumber; i++ {
				currContainerMemUsed += float64(containerStats[len(containerStats)-i].Memory.Usage)
			}
			podMemUsed += currContainerMemUsed / float64(dataSampleNumber)
		}
	} else {
		glog.Warningf("Cannot find pod %s", podNameWithNamespace)
		return nil, fmt.Errorf("Cannot find pod %s", podNameWithNamespace)
	}

	// convert num of core to frequency in MHz
	podCpuUsed = podCpuUsed * float64(cpuFrequency)
	podMemUsed = podMemUsed / 1024 // Mem is in bytes, convert to Kb

	glog.V(4).Infof("The actual Cpu used value of %s is %f", podNameWithNamespace, podCpuUsed)
	glog.V(4).Infof("The actual Mem used value of %s is %f", podNameWithNamespace, podMemUsed)

	cpuProvisionedUsed, memProvisionedUsed, err := GetResourceRequest(pod)
	if err != nil {
		return nil, fmt.Errorf("Error getting provisioned resource consumption: %s", err)
	}
	cpuProvisionedUsed *= float64(cpuFrequency)

	glog.V(3).Infof("Discovered pod is %s", podNameWithNamespace)
	glog.V(4).Infof("Pod %s CPU request is %f", podNameWithNamespace, podCpuUsed)
	glog.V(4).Infof("Pod %s Mem request is %f", podNameWithNamespace, podMemUsed)

	resourceStat := &PodResourceStat{
		vCpuCapacity:           podCpuCapacity,
		vCpuUsed:               podCpuUsed,
		vMemCapacity:           podMemCapacity,
		vMemUsed:               podMemUsed,
		cpuProvisionedCapacity: podCpuCapacity,
		cpuProvisionedUsed:     cpuProvisionedUsed,
		memProvisionedCapacity: podMemCapacity,
		memProvisionedUsed:     memProvisionedUsed,
	}

	podResourceConsumptionMap[podNameWithNamespace] = resourceStat

	return resourceStat, nil
}

// Build commodityDTOs for commodity sold by the pod
func (podProbe *PodProbe) getCommoditiesSold(pod *api.Pod, podResourceStat *PodResourceStat) (
	[]*proto.CommodityDTO, error) {

	podNameWithNamespace := pod.Namespace + "/" + pod.Name
	var commoditiesSold []*proto.CommodityDTO
	vMem, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
		Capacity(float64(podResourceStat.vMemCapacity)).
		Used(podResourceStat.vMemUsed).
		Resizable(true).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, vMem)

	vCpu, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
		Capacity(float64(podResourceStat.vCpuCapacity)).
		Used(podResourceStat.vCpuUsed).
		Resizable(true).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, vCpu)

	appComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(podNameWithNamespace).
		Capacity(1E10).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, appComm)
	return commoditiesSold, nil
}

// Build commodityDTOs for commodity sold by the pod
func (podProbe *PodProbe) getCommoditiesBought(pod *api.Pod, podResourceStat *PodResourceStat) (
	[]*proto.CommodityDTO, error) {

	var commoditiesBought []*proto.CommodityDTO
	vCpu, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
		Used(podResourceStat.vCpuUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, vCpu)

	vMem, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
		Used(podResourceStat.vMemUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, vMem)

	cpuProvisionedCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CPU_PROVISIONED).
		//		Key("Container").
		Used(podResourceStat.cpuProvisionedUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, cpuProvisionedCommBought)

	memProvisionedCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_MEM_PROVISIONED).
		//		Key("Container").
		Used(podResourceStat.memProvisionedUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, memProvisionedCommBought)

	selectorMap := pod.Spec.NodeSelector
	if len(selectorMap) > 0 {
		for key, value := range selectorMap {
			selector := key + "=" + value
			accessComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(selector).
				Create()
			if err != nil {
				return nil, err
			}
			commoditiesBought = append(commoditiesBought, accessComm)
		}
	}

	//cluster commodity
	clusterCommodityKey := ClusterID
	clusterComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterCommodityKey).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, clusterComm)
	return commoditiesBought, nil
}

// Build entityDTO that contains all the necessary info of a pod.
func (podProbe *PodProbe) buildPodEntityDTO(pod *api.Pod, commoditiesSold, commoditiesBought []*proto.CommodityDTO) (*proto.EntityDTO, error) {
	podNameWithNamespace := pod.Namespace + "/" + pod.Name

	id := podNameWithNamespace
	podDisplayName := podNameWithNamespace

	// TODO: quick fix. As "/" is not escaped on the new UI, id should be changed to "namespace:name".
	entityDTOBuilder := builder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, strings.Replace(podNameWithNamespace, "/", ":", -1))
	entityDTOBuilder.DisplayName(podDisplayName)

	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil, fmt.Errorf("Cannot find the hosting node ID for pod %s", podNameWithNamespace)
	}
	glog.V(4).Infof("Pod %s is hosted on %s", podDisplayName, nodeName)

	// TODO, temp solution.
	updateNodePodMap(nodeName, id)

	// sells commodities.
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	// buys commodities.
	providerNodeUID := nodeUIDMap[nodeName]
	provider := builder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, providerNodeUID)
	entityDTOBuilder = entityDTOBuilder.Provider(provider)
	entityDTOBuilder.BuysCommodities(commoditiesBought)

	// property
	property, err := podProbe.stitchingManager.BuildStitchingProperty(nodeName, stitching.Stitch)
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for Pod %s: %s", podDisplayName, err)
	}
	entityDTOBuilder = entityDTOBuilder.WithProperty(property)
	glog.V(4).Infof("Pod %s will be stitched with VM with %s: %s", podDisplayName, *property.Name, *property.Value)

	// monitored or not
	monitored := monitored(pod)
	if !monitored {
		inactivePods[podNameWithNamespace] = struct{}{}
	}
	entityDTOBuilder = entityDTOBuilder.Monitored(monitored)

	// create entityDTO
	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for pod %s: %s", id, err)
	}

	// Get the app type of the pod.
	podAppTypeMap[podNameWithNamespace] = GetAppType(pod)

	return entityDto, nil
}

func updateNodePodMap(nodeName, podID string) {
	var podIDs []string
	if ids, exist := nodePodMap[nodeName]; exist {
		podIDs = ids
	}
	podIDs = append(podIDs, podID)
	nodePodMap[nodeName] = podIDs
	podNodeMap[podID] = nodeName
}
