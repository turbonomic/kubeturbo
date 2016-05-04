package probe

import (
	"strings"

	vmtAdvisor "github.com/vmturbo/kubeturbo/pkg/cadvisor"
	vmtmonitor "github.com/vmturbo/kubeturbo/pkg/monitor"
	vmtproxy "github.com/vmturbo/kubeturbo/pkg/monitor"

	"github.com/golang/glog"
	info "github.com/google/cadvisor/info/v2"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var pod2AppMap map[string]map[string]vmtAdvisor.Application = make(map[string]map[string]vmtAdvisor.Application)

var podTransactionCountMap map[string]int = make(map[string]int)

type ApplicationProbe struct {
}

func NewApplicationProbe() *ApplicationProbe {
	return &ApplicationProbe{}
}

// Parse processes those are defined in namespace.
func (appProbe *ApplicationProbe) ParseApplication(namespace string) (result []*sdk.EntityDTO, err error) {
	glog.V(4).Infof("Has %d hosts", len(hostSet))

	transactionCountMap, err := appProbe.calculateTransactionValuePerPod()
	if err != nil {
		glog.Error(err)
		return
	}

	podTransactionCountMap = transactionCountMap

	for nodeName, host := range hostSet {

		glog.V(4).Infof("Now get process in host %s", nodeName)
		pod2ApplicationMap, err := appProbe.getApplicaitonPerPod(host)
		if err != nil {
			glog.Error(err)
			continue
		}

		for podName, appMap := range pod2ApplicationMap {
			pod2AppMap[podName] = appMap
		}

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

		for podName, appMap := range pod2ApplicationMap {
			for _, app := range appMap {
				glog.V(4).Infof("pod %s has the following application %s", podName, app.Cmd)

				appResourceStat := appProbe.getApplicationResourceStat(app, podName, nodeCpuCapacity, nodeMemCapacity, transactionCountMap)

				commoditiesSold := appProbe.getCommoditiesSold(app, appResourceStat)
				commoditiesBoughtMap := appProbe.getCommoditiesBought(podName, nodeName, appResourceStat)

				entityDto := appProbe.buildApplicationEntityDTOs(app, host, podName, nodeName, commoditiesSold, commoditiesBoughtMap)
				result = append(result, entityDto)
			}
		}
	}
	return
}

// Get transaction value for each pod. Returned map contains transaction values for all the pods in the cluster.
func (this *ApplicationProbe) calculateTransactionValuePerPod() (map[string]int, error) {
	transactionsCount, err := this.retrieveTransactions()
	if err != nil {
		return nil, err
	}

	var transactionCountMap map[string]int = make(map[string]int)

	for podIPAndPort, count := range transactionsCount {
		tempArray := strings.Split(podIPAndPort, ":")
		if len(tempArray) < 2 {
			glog.Errorf("Cannot get transaction count for endpoint %s", podIPAndPort)
			continue
		}
		podIP := tempArray[0]
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

// Find application running on each pod. Returned value is a map in the following format: {PodName, {ApplicationName, Applicaiton}}
func (this *ApplicationProbe) getApplicaitonPerPod(host *vmtAdvisor.Host) (map[string]map[string]vmtAdvisor.Application, error) {
	// use cadvisor to get all process on that host
	cadvisor := &vmtAdvisor.CadvisorSource{}
	psInfors, err := cadvisor.GetProcessInfo(*host)
	if err != nil {
		glog.Errorf("Error getting process from node with IP %s:%s", host.IP, err)
		return nil, err
	}

	pod2ApplicationMap := make(map[string]map[string]vmtAdvisor.Application)

	// If there is no process found, return an empty map
	if len(psInfors) < 1 {
		glog.Warningf("No process find")
		return pod2ApplicationMap, nil
	}

	// Now all process info have been got. Group processes to pods
	pod2ProcessesMap := make(map[string][]info.ProcessInfo)

	for _, process := range psInfors {
		// Here cgroupPath for a process is the same with the container name
		cgroupPath := process.CgroupPath
		if podName, exist := container2PodMap[cgroupPath]; exist {
			glog.V(5).Infof("%s is in pod %s", process.Cmd, podName)
			var processList []info.ProcessInfo
			if processes, hasList := pod2ProcessesMap[podName]; hasList {
				processList = processes
			}
			processList = append(processList, process)
			pod2ProcessesMap[podName] = processList
		}
	}

	// TODO, if only for defug purpose, there is no need to iterate when not debugging.
	for podN, processList := range pod2ProcessesMap {
		for _, process := range processList {
			glog.V(4).Infof("pod %s has the following process %s", podN, process.Cmd)
		}
	}

	// The same processes should represent the same application
	// key:podName, value: a map (key:process.Cmd, value: Application)
	for podName, processList := range pod2ProcessesMap {
		if _, exists := pod2ApplicationMap[podName]; !exists {
			apps := make(map[string]vmtAdvisor.Application)
			pod2ApplicationMap[podName] = apps
		}
		applications := pod2ApplicationMap[podName]
		for _, process := range processList {
			if _, hasApp := applications[process.Cmd]; !hasApp {
				applications[process.Cmd] = vmtAdvisor.Application(process)
			} else {
				app := applications[process.Cmd]
				app.PercentCpu = app.PercentCpu + process.PercentCpu
				app.PercentMemory = app.PercentMemory + process.PercentMemory
				applications[process.Cmd] = app
			}
		}
	}
	glog.V(4).Infof("pod2ApplicationMap is %++v", pod2ApplicationMap)

	return pod2ApplicationMap, nil
}

// Get resource usage status for a single application.
func (this *ApplicationProbe) getApplicationResourceStat(app vmtAdvisor.Application, podName string, nodeCpuCapacity, nodeMemCapacity float64, podTransactionCountMap map[string]int) *ApplicationResourceStat {
	cpuUsage := nodeCpuCapacity * float64(app.PercentCpu/100)
	memUsage := nodeMemCapacity * float64(app.PercentMemory/100)

	dispName := app.Cmd + "::" + podName

	glog.V(5).Infof("Percent Cpu for %s is %f, usage is %f", dispName, app.PercentCpu, cpuUsage)
	glog.V(5).Infof("Percent Mem for %s is %f, usage is %f", dispName, app.PercentMemory, memUsage)

	transactionCapacity := float64(1000)
	transactionUsed := float64(0)

	if count, ok := podTransactionCountMap[podName]; ok {
		transactionUsed = float64(count)
		glog.V(4).Infof("Get transactions value of pod %s, is %f", podName, transactionUsed)

	}

	if actionTestingFlag {
		transactionCapacity = float64(10000)
		transactionUsed = float64(9999)
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
func (this *ApplicationProbe) getCommoditiesSold(app vmtAdvisor.Application, appResourceStat *ApplicationResourceStat) []*sdk.CommodityDTO {
	var commoditiesSold []*sdk.CommodityDTO
	transactionComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_TRANSACTION).
		Key(app.Cmd).
		Capacity(appResourceStat.transactionCapacity).
		Used(appResourceStat.transactionUsed).
		Create()
	commoditiesSold = append(commoditiesSold, transactionComm)
	return commoditiesSold
}

// Build commodities bought by an applicaiton.
// An application buys vCpu and vMem from a VM, cpuAllocation and memAllocation from a containerPod.
func (this *ApplicationProbe) getCommoditiesBought(podName, nodeName string, appResourceStat *ApplicationResourceStat) map[*sdk.ProviderDTO][]*sdk.CommodityDTO {
	commoditiesBoughtMap := make(map[*sdk.ProviderDTO][]*sdk.CommodityDTO)

	podProvider := sdk.CreateProvider(sdk.EntityDTO_CONTAINER_POD, podName)
	var commoditiesBoughtFromPod []*sdk.CommodityDTO
	cpuAllocationCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_CPU_ALLOCATION).
		Key(podName).
		Used(appResourceStat.cpuAllocationUsed).
		Create()
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, cpuAllocationCommBought)
	memAllocationCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_MEM_ALLOCATION).
		Key(podName).
		Used(appResourceStat.memAllocationUsed).
		Create()
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, memAllocationCommBought)
	commoditiesBoughtMap[podProvider] = commoditiesBoughtFromPod

	nodeUID := nodeUidTranslationMap[nodeName]
	nodeProvider := sdk.CreateProvider(sdk.EntityDTO_VIRTUAL_MACHINE, nodeUID)
	var commoditiesBoughtFromNode []*sdk.CommodityDTO
	vCpuCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_VCPU).
		Key(nodeUID).
		Used(appResourceStat.vCpuUsed).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, vCpuCommBought)

	vMemCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_VMEM).
		Key(nodeUID).
		Used(appResourceStat.vMemUsed).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, vMemCommBought)
	appCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_APPLICATION).
		Key(nodeUID).
		Create()
	commoditiesBoughtFromNode = append(commoditiesBoughtFromNode, appCommBought)
	commoditiesBoughtMap[nodeProvider] = commoditiesBoughtFromNode

	return commoditiesBoughtMap
}

// Build entityDTOs for Applications.
func (this *ApplicationProbe) buildApplicationEntityDTOs(app vmtAdvisor.Application, host *vmtAdvisor.Host, podName, nodeName string, commoditiesSold []*sdk.CommodityDTO, commoditiesBoughtMap map[*sdk.ProviderDTO][]*sdk.CommodityDTO) *sdk.EntityDTO {
	appEntityType := sdk.EntityDTO_APPLICATION
	id := app.Cmd + "::" + podName
	dispName := app.Cmd + "::" + podName
	entityDTOBuilder := sdk.NewEntityDTOBuilder(appEntityType, id)
	entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)

	entityDTOBuilder.SellsCommodities(commoditiesSold)

	for provider, commodities := range commoditiesBoughtMap {
		entityDTOBuilder.SetProvider(provider)
		entityDTOBuilder.BuysCommodities(commodities)
	}

	entityDto := entityDTOBuilder.Create()

	appType := app.Cmd

	ipAddress := this.getIPAddress(host, nodeName)

	appData := &sdk.EntityDTO_ApplicationData{
		Type:      &appType,
		IpAddress: &ipAddress,
	}
	entityDto.ApplicationData = appData
	return entityDto
}

// Get transaction values for each endpoint. Return a map, {endpointIP, transactionCount}
func (this *ApplicationProbe) retrieveTransactions() (map[string]int, error) {
	servicesTransactions, err := this.getTransactionFromAllNodes()
	if err != nil {
		return nil, err
	}

	ep2TransactionCountMap := make(map[string]int)
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
