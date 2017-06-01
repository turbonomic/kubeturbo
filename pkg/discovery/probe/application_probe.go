package probe

import (
	"fmt"

	vmtAdvisor "github.com/turbonomic/kubeturbo/pkg/cadvisor"
	vmtmonitor "github.com/turbonomic/kubeturbo/pkg/monitor"
	vmtproxy "github.com/turbonomic/kubeturbo/pkg/monitor"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const appPrefix string = "App-"

var podTransactionCountMap map[string]float64

type ApplicationProbe struct{}

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

	for podNamespaceName := range podResourceConsumptionMap {
		appResourceStat := appProbe.getApplicationResourceStatFromPod(podNamespaceName, transactionCountMap)

		commoditiesSold, err := appProbe.getCommoditiesSold(podNamespaceName, appResourceStat)
		if err != nil {
			return nil, err
		}
		commoditiesBoughtMap, err := appProbe.getCommoditiesBought(podNamespaceName, appResourceStat)
		if err != nil {
			return nil, err
		}

		entityDto, err := appProbe.buildApplicationEntityDTOs(podNamespaceName, podNamespaceName, commoditiesSold, commoditiesBoughtMap)
		if err != nil {
			return nil, err
		}
		result = append(result, entityDto)
		glog.V(4).Infof("Build Application based on pod %s", podNamespaceName)
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
func (this *ApplicationProbe) getApplicationResourceStatFromPod(podNamespaceName string, podTransactionCountMap map[string]float64) *ApplicationResourceStat {
	podResourceStat := podResourceConsumptionMap[podNamespaceName]

	cpuUsage := podResourceStat.vCpuUsed
	memUsage := podResourceStat.vMemUsed

	transactionCapacity := float64(50)
	transactionUsed := float64(0)

	if count, ok := podTransactionCountMap[podNamespaceName]; ok {
		transactionUsed = count
		glog.V(4).Infof("Get transactions value of pod %s, is %f", podNamespaceName, transactionUsed)
	}

	return &ApplicationResourceStat{
		vCpuUsed:            cpuUsage,
		vMemUsed:            memUsage,
		transactionCapacity: transactionCapacity,
		transactionUsed:     transactionUsed,
	}
}

// Build commodities sold for each application. An application sells transaction, which a virtual application buys.
func (this *ApplicationProbe) getCommoditiesSold(podNamespaceName string, appResourceStat *ApplicationResourceStat) (
	[]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	appType := podAppTypeMap[podNamespaceName]
	transactionComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_TRANSACTION).
		Key(appType + "-" + ClusterID).
		Capacity(appResourceStat.transactionCapacity).
		Used(appResourceStat.transactionUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, transactionComm)
	return commoditiesSold, nil
}

// Build commodities bought by an application.
// An application buys vCpu, vMem and application commodity from a containerPod.
func (this *ApplicationProbe) getCommoditiesBought(podNamespaceName string, appResourceStat *ApplicationResourceStat) (
	map[*builder.ProviderDTO][]*proto.CommodityDTO, error) {

	commoditiesBoughtMap := make(map[*builder.ProviderDTO][]*proto.CommodityDTO)

	turboPodUUID, exist := turboPodUUIDMap[podNamespaceName]
	if !exist {
		return nil, fmt.Errorf("Cannot build commodity bought based on given Pod identifier: %s.", podNamespaceName)
	}

	podProvider := builder.CreateProvider(proto.EntityDTO_CONTAINER_POD, turboPodUUID)
	var commoditiesBoughtFromPod []*proto.CommodityDTO
	vCpuCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
		Used(appResourceStat.vCpuUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, vCpuCommBought)

	vMemCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
		Used(appResourceStat.vMemUsed).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, vMemCommBought)

	appCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(turboPodUUID).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBoughtFromPod = append(commoditiesBoughtFromPod, appCommBought)
	commoditiesBoughtMap[podProvider] = commoditiesBoughtFromPod

	return commoditiesBoughtMap, nil
}

// Build entityDTOs for Applications.
func (this *ApplicationProbe) buildApplicationEntityDTOs(appName, podNamespaceName string, commoditiesSold []*proto.CommodityDTO,
	commoditiesBoughtMap map[*builder.ProviderDTO][]*proto.CommodityDTO) (*proto.EntityDTO, error) {

	turboPodUUID, exist := turboPodUUIDMap[podNamespaceName]
	if !exist {
		return nil, fmt.Errorf("Cannot build application entityDTO based on give pod identifier: %s. "+
			"Failed to find Turbo UUID.", podNamespaceName)
	}
	id := appPrefix + turboPodUUID
	appEntityType := proto.EntityDTO_APPLICATION
	displayName := appName
	entityDTOBuilder := builder.NewEntityDTOBuilder(appEntityType, id)
	entityDTOBuilder = entityDTOBuilder.DisplayName("App-" + displayName)

	entityDTOBuilder.SellsCommodities(commoditiesSold)
	for provider, commodities := range commoditiesBoughtMap {
		entityDTOBuilder = entityDTOBuilder.Provider(provider).BuysCommodities(commodities)
	}

	appType := podAppTypeMap[podNamespaceName]
	appData := &proto.EntityDTO_ApplicationData{
		Type: &appType,
	}
	entityDTOBuilder.ApplicationData(appData)

	if _, exist := inactivePods[podNamespaceName]; exist {
		entityDTOBuilder = entityDTOBuilder.Monitored(false)
	}

	appProperties := buildAppProperties(podNamespaceName)
	entityDTOBuilder = entityDTOBuilder.WithProperties(appProperties)

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for application %s: %s", id, err)
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
