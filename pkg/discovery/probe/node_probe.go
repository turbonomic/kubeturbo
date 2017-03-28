package probe

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"

	cadvisor "github.com/google/cadvisor/info/v1"

	vmtAdvisor "github.com/turbonomic/kubeturbo/pkg/cadvisor"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	proxyVMIP string = "Proxy_VM_IP"
)

var hostSet map[string]*vmtAdvisor.Host = make(map[string]*vmtAdvisor.Host)

var nodeUidTranslationMap map[string]string = make(map[string]string)

var nodeName2ExternalIPMap map[string]string = make(map[string]string)

// A map stores the machine information of each node. Key is node name, value is the corresponding machine information.
var nodeMachineInfoMap map[string]*cadvisor.MachineInfo = make(map[string]*cadvisor.MachineInfo)

// A node probe discoveries nodes inside a Kubernetes cluster and build entityDTOs based on discovered information.
type NodeProbe struct {
	nodesAccessor ClusterAccessor

	// The probe config is used to build cAdvisor client.
	config *ProbeConfig

	nodeIPMap map[string]map[api.NodeAddressType]string
}

// Since this is only used in probe package, do not expose it.
func NewNodeProbe(accessor ClusterAccessor, config *ProbeConfig) *NodeProbe {
	return &NodeProbe{
		nodesAccessor: accessor,
		config:        config,
	}
}

// Parse each node inside K8s. Get the resources usage of each node and build the entityDTO.
func (nodeProbe *NodeProbe) parseNodeFromK8s(nodes []*api.Node, pods []*api.Pod) (result []*proto.EntityDTO, err error) {
	nodePodsMap := make(map[string][]*api.Pod)
	for _, pod := range pods {
		var podList []*api.Pod
		if l, exist := nodePodsMap[pod.Spec.NodeName]; exist {
			podList = l
		}
		podList = append(podList, pod)
		nodePodsMap[pod.Spec.NodeName] = podList
	}
	for _, node := range nodes {
		// We do not parse node that is not ready or unschedulable.
		if !nodeIsReady(node) || !nodeIsSchedulable(node) {
			glog.V(3).Infof("Node %s is either not ready or unschedulable. Skip", node.Name)
			continue
		}
		// use cAdvisor to get node info
		nodeProbe.parseNodeIP(node)
		hostSet[node.Name] = nodeProbe.getHost(node.Name)

		// // Now start to build supply chain.
		nodeID := string(node.UID)
		dispName := node.Name
		nodeUidTranslationMap[node.Name] = nodeID

		commoditiesSold, err := nodeProbe.createCommoditySold(node, nodePodsMap)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for %s: %s", node.Name, err)
			continue
		}
		entityDto, err := nodeProbe.buildVMEntityDTO(nodeID, dispName, commoditiesSold)
		if err != nil {
			return nil, err
		}

		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(4).Infof("Node EntityDTO: " + entityDto.GetDisplayName())
		for _, c := range entityDto.CommoditiesSold {
			glog.V(5).Infof("Node commodity type is " + strconv.Itoa(int(c.GetCommodityType())) + "\n")
		}
	}

	return
}

// Check is a node is ready.
func nodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

// Check if a node is schedulable.
func nodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}

func (nodeProbe *NodeProbe) createCommoditySold(node *api.Node, nodePodsMap map[string][]*api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	nodeResourceStat, err := nodeProbe.getNodeResourceStat(node, nodePodsMap)
	if err != nil {
		return commoditiesSold, err
	}
	nodeID := string(node.UID)
	vMemComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
		Capacity(nodeResourceStat.vMemCapacity).
		Used(nodeResourceStat.vMemUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, vMemComm)

	vCpuComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
		Capacity(float64(nodeResourceStat.vCpuCapacity)).
		Used(nodeResourceStat.vCpuUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, vCpuComm)

	memProvisionedComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_MEM_PROVISIONED).
		//		Key("Container").
		Capacity(float64(nodeResourceStat.memProvisionedCapacity)).
		Used(nodeResourceStat.memProvisionedUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, memProvisionedComm)

	cpuProvisionedComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CPU_PROVISIONED).
		//		Key("Container").
		Capacity(float64(nodeResourceStat.cpuProvisionedCapacity)).
		Used(nodeResourceStat.cpuProvisionedUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, cpuProvisionedComm)

	appComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(nodeID).
		Capacity(1E10).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, appComm)

	labelsmap := node.ObjectMeta.Labels
	if len(labelsmap) > 0 {
		for key, value := range labelsmap {
			label := key + "=" + value
			glog.V(4).Infof("label for this Node is : %s", label)
			accessComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(label).
				Capacity(1E10).
				Create()
			if err != nil {
				return nil, err
			}
			commoditiesSold = append(commoditiesSold, accessComm)
		}
	}

	// Use Kubernetes service UID as the key for cluster commodity
	clusterCommodityKey := ClusterID
	clusterComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterCommodityKey).
		Capacity(1E10).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, clusterComm)

	return commoditiesSold, nil
}

func (nodeProbe *NodeProbe) getHost(nodeName string) *vmtAdvisor.Host {
	nodeIP, err := nodeProbe.getNodeIPWithType(nodeName, api.NodeLegacyHostIP)
	if err != nil {
		glog.Errorf("Error getting NodeLegacyHostIP of node %s: %s.", nodeName, err)
		return nil
	}
	// Use NodeLegacyHostIP to build the host to interact with cAdvisor.
	host := &vmtAdvisor.Host{
		IP:   nodeIP,
		Port: nodeProbe.config.CadvisorPort,
		//Resource: "",
	}
	return host
}

// Retrieve the legacyHostIP of each node and put other IPs to related maps.
func (nodeProbe *NodeProbe) parseNodeIP(node *api.Node) {
	if nodeProbe.nodeIPMap == nil {
		nodeProbe.nodeIPMap = make(map[string]map[api.NodeAddressType]string)
	}
	currentNodeIPMap := make(map[api.NodeAddressType]string)

	nodeAddresses := node.Status.Addresses
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == api.NodeExternalIP {
			nodeName2ExternalIPMap[node.Name] = nodeAddress.Address
		}
		currentNodeIPMap[nodeAddress.Type] = nodeAddress.Address
	}

	nodeProbe.nodeIPMap[node.Name] = currentNodeIPMap
}

func (this *NodeProbe) getNodeIPWithType(nodeName string, ipType api.NodeAddressType) (string, error) {
	ips, ok := this.nodeIPMap[nodeName]
	if !ok {
		return "", fmt.Errorf("IP of node %s is not tracked", nodeName)
	}
	nodeIP, exist := ips[ipType]
	if !exist {
		return "", fmt.Errorf("node %s does not have %s.", nodeName, ipType)
	}
	return nodeIP, nil
}

func (nodeProbe *NodeProbe) buildVMEntityDTO(nodeID, displayName string, commoditiesSold []*proto.CommodityDTO) (*proto.EntityDTO, error) {
	entityDTOBuilder := builder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, nodeID)
	entityDTOBuilder.DisplayName(displayName)
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	ipAddress := nodeProbe.getIPForStitching(displayName)
	propertyName := proxyVMIP
	// TODO
	propertyNamespace := "DEFAULT"
	entityDTOBuilder = entityDTOBuilder.WithProperty(&proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &ipAddress,
	})
	glog.V(4).Infof("Parse node: The ip of vm to be reconcile with is %s", ipAddress)

	metaData := generateReconcilationMetaData()
	entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

	entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for node %s: %s", nodeID, err)
	}

	return entityDto, nil
}

// Create the meta data that will be used during the reconciliation process.
func generateReconcilationMetaData() *proto.EntityDTO_ReplacementEntityMetaData {
	replacementEntityMetaDataBuilder := builder.NewReplacementEntityMetaDataBuilder()
	replacementEntityMetaDataBuilder.Matching(proxyVMIP)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_CPU_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_MEM_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_APPLICATION)
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_VMPM_ACCESS)

	metaData := replacementEntityMetaDataBuilder.Build()
	return metaData
}

// Get the correct IP that will be used during the stitching process.
func (nodeProbe *NodeProbe) getIPForStitching(nodeName string) string {
	// If this is a local test, just return the predefined local testing stitching IP.
	if localTestingFlag {
		return localTestStitchingIP
	}
	nodeIPs, exist := nodeProbe.nodeIPMap[nodeName]
	if !exist {
		glog.Fatalf("Error trying to get IP of node %s in Kubernetes cluster", nodeName)
	}
	if externalIP, ok := nodeIPs[api.NodeExternalIP]; ok {
		return externalIP
	}

	return nodeIPs[api.NodeLegacyHostIP]
}

// Get current stat of node resources, such as capacity and used values.
func (this *NodeProbe) getNodeResourceStat(node *api.Node, nodePodsMap map[string][]*api.Pod) (*NodeResourceStat, error) {
	cadvisor := &vmtAdvisor.CadvisorSource{}

	host := this.getHost(node.Name)
	machineInfo, err := cadvisor.GetMachineInfo(*host)
	if err != nil {
		return nil, fmt.Errorf("Error getting machineInfo for %s: %s", node.Name, err)
	}
	// The return cpu frequency is in KHz, we need MHz
	cpuFrequency := machineInfo.CpuFrequency / 1000
	nodeMachineInfoMap[node.Name] = machineInfo

	// Here we only need the root container.
	_, root, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
	if err != nil {
		return nil, fmt.Errorf("Error getting root container info for %s: %s", node.Name, err)
	}
	containerStats := root.Stats
	// To get a valid cpu usage, there must be at least 2 valid stats.
	if len(containerStats) < 2 {
		glog.Warning("Not enough data")
		return nil, fmt.Errorf("Not enough status data of current node %s.", node.Name)
	}
	currentStat := containerStats[len(containerStats)-1]
	prevStat := containerStats[len(containerStats)-2]
	rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
	glog.V(4).Infof("rawUsage is %d", rawUsage)
	intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
	glog.V(4).Infof("interval is %d", intervalInNs)
	rootCurCpu := float64(rawUsage) * 1.0 / float64(intervalInNs)
	rootCurMem := float64(currentStat.Memory.Usage) / 1024 // Mem is returned in B

	// Get the node Cpu and Mem capacity.
	nodeCpuCapacity := float64(machineInfo.NumCores) * float64(cpuFrequency)
	nodeMemCapacity := float64(machineInfo.MemoryCapacity) / 1024 // Mem is returned in B
	glog.V(3).Infof("Discovered node is " + node.Name)
	glog.V(4).Infof("Node CPU capacity is %f", nodeCpuCapacity)
	glog.V(4).Infof("Node Mem capacity is %f", nodeMemCapacity)
	// Find out the used value for each commodity
	cpuUsed := float64(rootCurCpu) * float64(cpuFrequency)
	memUsed := float64(rootCurMem)

	cpuProvisionedUsed := float64(0)
	memProvisionedUsed := float64(0)

	if podList, exist := nodePodsMap[node.Name]; exist {
		for _, pod := range podList {
			cpuRequest, memRequest, err := GetResourceRequest(pod)
			if err != nil {
				return nil, fmt.Errorf("Error getting provisioned resource consumption: %s", err)
			}
			cpuProvisionedUsed += cpuRequest
			memProvisionedUsed += memRequest
		}
	}
	cpuProvisionedUsed *= float64(cpuFrequency)

	return &NodeResourceStat{
		vCpuCapacity:           nodeCpuCapacity,
		vCpuUsed:               cpuUsed,
		vMemCapacity:           nodeMemCapacity,
		vMemUsed:               memUsed,
		cpuProvisionedCapacity: nodeCpuCapacity,
		cpuProvisionedUsed:     cpuProvisionedUsed,
		memProvisionedCapacity: nodeMemCapacity,
		memProvisionedUsed:     memProvisionedUsed,
	}, nil
}
