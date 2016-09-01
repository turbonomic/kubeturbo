package probe

import (
	"strings"

	vmtAdvisor "github.com/vmturbo/kubeturbo/pkg/cadvisor"
	"github.com/vmturbo/kubeturbo/pkg/helper"
	vmtmonitor "github.com/vmturbo/kubeturbo/pkg/monitor"
	vmtproxy "github.com/vmturbo/kubeturbo/pkg/monitor"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/builder"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/common"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const appPrefix string = "App-"

var podTransactionCountMap map[string]float64 = make(map[string]float64)

type ApplicationProbe struct {
}

func NewApplicationProbe() *ApplicationProbe {
	return &ApplicationProbe{}
}

// Parse processes those are defined in namespace.
func (appProbe *ApplicationProbe) ParseApplication(namespace string) (result []*proto.EntityDTO, err error) {
	glog.V(4).Infof("Has %d hosts", len(hostSet))

	transactionCountMap, err := appProbe.calculateTransactionValuePerPod()
	if err != nil {
		glog.Error(err)
		return
	}

	podTransactionCountMap = transactionCountMap

	for nodeName, host := range hostSet {

		// In order to get the actual usage for each process, the CPU/Mem capacity
		// for the machine must be retrieved.
		machineInfo, exist := nodeMachineInfoMap[nodeName]
		if !exist {
			glog.Warningf("Error getting machine info for %s when parsing process: %s", nodeName, err)
			continue
			// return nil, err
		}
		// The return cpu frequency is in KHz, we need MHz
		cpuFrequency := machineInfo.CpuFrequency / 1000
		// Get the node Cpu and Mem capacity.
		nodeCpuCapacity := float64(machineInfo.NumCores) * float64(cpuFrequency)
		nodeMemCapacity := float64(machineInfo.MemoryCapacity) / 1024 // Mem is returned in B

		for podName, _ := range podResourceConsumptionMap {
			appResourceStat := appProbe.getApplicationResourceStatFromPod(podName, nodeCpuCapacity, nodeMemCapacity, transactionCountMap)

			commoditiesSold := appProbe.getCommoditiesSold(podName, appResourceStat)
			commoditiesBoughtMap := appProbe.getCommoditiesBought(podName, nodeName, appResourceStat)

			entityDto, err := appProbe.buildApplicationEntityDTOs(podName, host, podName, nodeName, commoditiesSold, commoditiesBoughtMap)
			if err != nil {
				return nil, err
			}
			result = append(result, entityDto)
		}
	}
	return
}

// Get transaction value for each pod. Returned map contains transaction values for all the pods in the cluster.
func (this *ApplicationProbe) calculateTransactionValuePerPod() (map[string]float64, error) {
	transactionsCount, err := this.retrieveTransactions()
	if err != nil {
		return nil, err
	}

	var transactionCountMap map[string]float64 = make(map[string]float64)

	for podIPAndPort, count := range transactionsCount {
		podIP := podIPAndPort
		pod, ok := podIP2PodMap[podIP]
		if !ok {
			glog.Errorf("Cannot link pod with IP %s in the podSet", podIP)
			continue
		}
		podNameWithNamespace := pod.Namespace + "/" + pod.Name
		transactionCountMap[podNameWithNamespace] = count

	}
	glog.V(5).Infof("transactionCountMap is %++v", transactionCountMap)
	return transactionCountMap, nil
}

// Get resource usage status for a single application.
func (this *ApplicationProbe) getApplicationResourceStatFromPod(podName string, nodeCpuCapacity, nodeMemCapacity float64, podTransactionCountMap map[string]float64) *ApplicationResourceStat {
	podResourceStat := podResourceConsumptionMap[podName]

	cpuUsage := podResourceStat.cpuAllocationUsed
	memUsage := podResourceStat.memAllocationUsed

	transactionCapacity := float64(1000)
	transactionUsed := float64(0)

	if count, ok := podTransactionCountMap[podName]; ok {
		transactionUsed = count
		glog.V(4).Infof("Get transactions value of pod %s, is %f", podName, transactionUsed)
	}

	flag, err := helper.LoadTestingFlag()
	if err == nil {
		if flag.ProvisionTestingFlag {
			if fakeUtil := flag.FakeTransactionUtil; fakeUtil != 0 {
				transactionUsed = fakeUtil * transactionCapacity
			}
		} else if flag.DeprovisionTestingFlag {
			if fakeUtil := flag.FakeTransactionUtil; fakeUtil != 0 {
				transactionUsed = fakeUtil * transactionCapacity
			}
			if fakeCpuUsed := flag.FakeApplicationCpuUsed; fakeCpuUsed != 0 {
				cpuUsage = fakeCpuUsed
			}
			if fakeMemUsed := flag.FakeApplicationMemUsed; fakeMemUsed != 0 {
				memUsage = fakeMemUsed
			}
		}
	}

	return &ApplicationResourceStat{
		cpuAllocationUsed:   cpuUsage,
		memAllocationUsed:   memUsage,
		vCpuUsed:            cpuUsage,
		vMemUsed:            memUsage,
		transactionCapacity: transactionCapacity,
		transactionUsed:     transactionUsed,
	}
}

// Build commodities sold for each application. An application sells transaction, which a virtual application buys.
func (this *ApplicationProbe) getCommoditiesSold(appName string, appResourceStat *ApplicationResourceStat) []*proto.CommodityDTO {
	var commoditiesSold []*proto.CommodityDTO
	transactionComm := builder.NewCommodityDTOBuilder(proto.CommodityDTO_TRANSACTION).
		Key(appName).
		Capacity(appResourceStat.transactionCapacity).
		Used(appResourceStat.transactionUsed).
		Create()
	commoditiesSold = append(commoditiesSold, transactionComm)
	return commoditiesSold
}

// Build commodities bought by an applicaiton.
// An application buys vCpu and vMem from a VM, cpuAllocation and memAllocation from a containerPod.
func (this *ApplicationProbe) getCommoditiesBought(podName, nodeName string, appResourceStat *ApplicationResourceStat) map[*common.ProviderDTO][]*proto.CommodityDTO {
	commoditiesBoughtMap := make(map[*common.ProviderDTO][]*proto.CommodityDTO)

	// NOTE: quick fix, podName are now show as namespace:name, which is namespace/name before. So we need to replace "/" with ":".
	podProvider := common.CreateProvider(proto.EntityDTO_CONTAINER_POD, strings.Replace(podName, "/", ":", -1))
	var commoditiesBoughtFromPod []*proto.CommodityDTO
	cpuAllocationCommBought := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CPU_ALLOCATION).
		Key(podName).
		Used(appResourceStat.cpuAllocationUsed).
		Create()
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, cpuAllocationCommBought)
	memAllocationCommBought := builder.NewCommodityDTOBuilder(proto.CommodityDTO_MEM_ALLOCATION).
		Key(podName).
		Used(appResourceStat.memAllocationUsed).
		Create()
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, memAllocationCommBought)
	commoditiesBoughtMap[podProvider] = commoditiesBoughtFromPod

	nodeUID := nodeUidTranslationMap[nodeName]
	nodeProvider := common.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, nodeUID)
	var commoditiesBoughtFromNode []*proto.CommodityDTO
	vCpuCommBought := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
		Used(appResourceStat.vCpuUsed).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, vCpuCommBought)

	vMemCommBought := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
		Used(appResourceStat.vMemUsed).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, vMemCommBought)
	appCommBought := builder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(nodeUID).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, appCommBought)
	commoditiesBoughtMap[nodeProvider] = commoditiesBoughtFromNode

	return commoditiesBoughtMap
}

// Build entityDTOs for Applications.
func (this *ApplicationProbe) buildApplicationEntityDTOs(appName string, host *vmtAdvisor.Host, podName, nodeName string, commoditiesSold []*proto.CommodityDTO, commoditiesBoughtMap map[*common.ProviderDTO][]*proto.CommodityDTO) (*proto.EntityDTO, error) {
	appEntityType := proto.EntityDTO_APPLICATION
	id := appPrefix + appName
	dispName := appName
	entityDTOBuilder := builder.NewEntityDTOBuilder(appEntityType, strings.Replace(id, "/", ":", -1))
	entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)

	entityDTOBuilder.SellsCommodities(commoditiesSold)

	for provider, commodities := range commoditiesBoughtMap {
		entityDTOBuilder.SetProvider(provider)
		entityDTOBuilder.BuysCommodities(commodities)
	}

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, err
	}

	appType := podAppTypeMap[podName]

	ipAddress := this.getIPAddress(host, nodeName)

	appData := &proto.EntityDTO_ApplicationData{
		Type:      &appType,
		IpAddress: &ipAddress,
	}
	entityDto.ApplicationData = appData

	if _, exist := inactivePods[podName]; exist {
		monitored := false
		entityDto.Monitored = &monitored
	}
	return entityDto, nil
}

// Get transaction values for each endpoint. Return a map, {endpointIP, transactionCount}
func (this *ApplicationProbe) retrieveTransactions() (map[string]float64, error) {
	servicesTransactions, err := this.getTransactionFromAllNodes()
	if err != nil {
		return nil, err
	}

	ep2TransactionCountMap := make(map[string]float64)
	for _, transaction := range servicesTransactions {
		epCounterMap := transaction.GetEndpointsCounterMap()
		for ep, count := range epCounterMap {
			curCount, exist := ep2TransactionCountMap[ep]
			if !exist {
				curCount = 0
			}
			curCount = curCount + count
			ep2TransactionCountMap[ep] = curCount
		}
	}
	return ep2TransactionCountMap, nil
}

// Get transactions values from all hosts.
func (this *ApplicationProbe) getTransactionFromAllNodes() (transactionInfo []vmtproxy.Transaction, err error) {
	for nodeName, host := range hostSet {
		transactions, err := this.getTransactionFromNode(host)
		if err != nil {
			glog.Errorf("error: %s", err)
			// TODO, do not return in order to not block the discover in other host.
			continue
		}
		if len(transactions) < 1 {
			glog.V(3).Infof("No transaction data in %s.", nodeName)
			continue
		}
		glog.V(4).Infof("Transactions from %s are: %v", nodeName, transactions)

		transactionInfo = append(transactionInfo, transactions...)
	}
	return transactionInfo, nil
}

// Get transaction value from a single host.
func (this *ApplicationProbe) getTransactionFromNode(host *vmtAdvisor.Host) ([]vmtproxy.Transaction, error) {
	glog.V(4).Infof("Now get transactions in host %s", host.IP)
	monitor := &vmtmonitor.ServiceMonitor{}
	transactions, err := monitor.GetServiceTransactions(*host)
	if err != nil {
		glog.Errorf("Error getting transaction data from %s: %s", host.IP, err)
		return transactions, err
	}
	return transactions, nil
}

func (this *ApplicationProbe) getIPAddress(host *vmtAdvisor.Host, nodeName string) string {
	if localTestingFlag {
		return localTestStitchingIP
	}
	ipAddress := host.IP
	if externalIP, ok := nodeName2ExternalIPMap[nodeName]; ok {
		ipAddress = externalIP
	}
	glog.V(4).Infof("Parse application: The ip of vm to be stitched is %s", ipAddress)

	return ipAddress
}
