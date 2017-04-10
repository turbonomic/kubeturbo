package discovery

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"errors"
	"github.com/golang/glog"
	"github.com/pborman/uuid"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)



type k8sDiscoveryWorkerConfig struct {
	kubeConfig *restclient.Config

	monitoringSourceConfigs map[types.MonitorType][]monitoring.MonitorWorkerConfig
}

func NewK8sDiscoveryWorkerConfig(kubeConfig *restclient.Config) *k8sDiscoveryWorkerConfig {
	return &k8sDiscoveryWorkerConfig{
		kubeConfig:              kubeConfig,
		monitoringSourceConfigs: make(map[types.MonitorType][]monitoring.MonitorWorkerConfig),
	}
}

func (c *k8sDiscoveryWorkerConfig) WithMonitoringWorkerConfig(config monitoring.MonitorWorkerConfig) *k8sDiscoveryWorkerConfig {
	monitorType := config.GetMonitorType()
	configs, exist := c.monitoringSourceConfigs[monitorType]
	if !exist {
		configs = []monitoring.MonitorWorkerConfig{}
	}

	configs = append(configs, config)
	c.monitoringSourceConfigs[monitorType] = configs

	return c
}

type k8sDiscoveryWorker struct {
	id string

	//clusterAccessor probe.ClusterAccessor
	kubeClient *client.Client

	monitoringWorker map[types.MonitorType][]monitoring.MonitoringWorker

	sink *metrics.EntityMetricSink

	// Currently a worker only receives one task at a time.
	task *task.Task
}

func NewK8sDiscoveryWorker(config *k8sDiscoveryWorkerConfig) (*k8sDiscoveryWorker, error) {
	id := uuid.NewUUID().String()

	kubeClient, err := client.New(config.kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid API configuration: %v", err)
	}

	if len(config.monitoringSourceConfigs) == 0 {
		return nil, errors.New("No monitoring source config found in config.")
	}

	// Build all the monitoring worker based on configs.
	monitoringWorkerMap := make(map[types.MonitorType][]monitoring.MonitoringWorker)
	for monitorType, configList := range config.monitoringSourceConfigs {
		monitorList, exist := monitoringWorkerMap[monitorType]
		if !exist {
			monitorList = []monitoring.MonitoringWorker{}
		}
		for _, config := range configList {
			monitoringWorker, err := monitoring.BuildMonitorWorker(config.GetMonitoringSource(), config)
			if err != nil {
				// TODO return?
				glog.Errorf("Failed to build monitoring worker configuration: %v", err)
				continue
			}
			monitorList = append(monitorList, monitoringWorker)
		}
		monitoringWorkerMap[monitorType] = monitorList
	}

	return &k8sDiscoveryWorker{
		id:               id,
		kubeClient:       kubeClient,
		monitoringWorker: monitoringWorkerMap,
		sink:             metrics.NewEntityMetricSink(),
	}, nil
}

func (worker *k8sDiscoveryWorker) AssignTask(task *task.Task) error {
	if worker.task != nil {
		return fmt.Errorf("The current worker %s has already been assigned a task and has not finished yet", worker.id)
	}
	worker.task = task
	return nil
}

// Worker start to working on task.
func (worker *k8sDiscoveryWorker) Do() {
	if worker.task == nil {
		glog.Error("No task has been assigned to current worker.")
		return
	}

	err := worker.findClusterID()

	// Resource monitoring
	resourceMonitorTask := worker.task
	if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.ResourceMonitor]; exist {
		for _, rmWorker := range resourceMonitoringWorkers {
			// Assign task to monitoring worker.
			rmWorker.AssignTask(resourceMonitorTask)
			monitoringSink := rmWorker.Do()
			// Don't do any filtering
			worker.sink.MergeSink(monitoringSink, nil)
		}
	}

	// Get all pods in the task scope
	//pods := worker.findPodsRunningInTaskNodes()

	// Topology monitoring
	clusterMonitorTask := worker.task
	if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.StateMonitor]; exist {
		for _, smWorker := range resourceMonitoringWorkers {
			// Assign task to monitoring worker.
			smWorker.AssignTask(clusterMonitorTask)
			monitoringSink := smWorker.Do()
			// Don't do any filtering
			worker.sink.MergeSink(monitoringSink, nil)
		}
	}

	// Build EntityDTO

	// Send result

}

func (worker *k8sDiscoveryWorker) getNodeCommoditiesSold(node api.Node) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	key := util.NodeKeyFunc(node)

	resourceCommoditiesSold, err := worker.getResourceCommoditiesSold(task.NodeType, key, rTypeMapping)
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	// VMPM_ACCESS
	labelMetricUID := metrics.GenerateEntityStateMetricUID(task.NodeType, key, metrics.Access)
	labelMetric, err := worker.sink.GetMetric(labelMetricUID)
	if err != nil {
		glog.Errorf("Failed to get %s used for %s %s", metrics.Access, task.NodeType, key)
	} else {
		labelPairs, ok := labelMetric.GetValue().([]string)
		if !ok {
			glog.Errorf("Failed to get label pairs for %s %s", task.NodeType, key)
		}
		for _, label := range labelPairs {
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

	// APPLICATION
	appComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(key).
		Capacity(1E10).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, appComm)

	// CLUSTER
	// Use Kubernetes service UID as the key for cluster commodity
	clusterCommodityKey, err := getClusterID(worker.kubeClient)
	if err != nil {
		glog.Error("Failed to get cluster ID")
	} else {
		clusterComm, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
			Key(clusterCommodityKey).
			Capacity(1E10).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesSold = append(commoditiesSold, clusterComm)
	}

	return commoditiesSold, nil
}

func (worker *k8sDiscoveryWorker) getResourceCommoditiesSold(entityType task.DiscoveredEntityType, entityID string,
	rTypesMapping map[metrics.ResourceType]proto.CommodityDTO_CommodityType) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for rType, cType := range rTypesMapping {
		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := worker.sink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s", rType, entityType, entityID)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)

		capacityUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Capacity)
		capacityMetric, err := worker.sink.GetMetric(capacityUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s capacity for %s %s", rType, entityType, entityID)
			continue
		}
		vcpuCapacityValue := capacityMetric.GetValue().(float64)

		commSold, err := builder.NewCommodityDTOBuilder(cType).
			Capacity(vcpuCapacityValue).
			Used(usedValue).
			Create()
		if err != nil {
			// TODO return?
			return nil, err
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (worker *k8sDiscoveryWorker) getResourceCommoditiesBought(entityType task.DiscoveredEntityType, entityID string,
	rTypesMapping map[metrics.ResourceType]proto.CommodityDTO_CommodityType) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for rType, cType := range rTypesMapping {
		usedMetricUID := metrics.GenerateEntityResourceMetricUID(entityType, entityID, rType, metrics.Used)
		usedMetric, err := worker.sink.GetMetric(usedMetricUID)
		if err != nil {
			// TODO return?
			glog.Errorf("Failed to get %s used for %s %s", rType, entityType, entityID)
			continue
		}
		usedValue := usedMetric.GetValue().(float64)

		commSold, err := builder.NewCommodityDTOBuilder(cType).
			Used(usedValue).
			Create()
		if err != nil {
			// TODO return?
			return nil, err
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}
//
func (worker *k8sDiscoveryWorker) buildVMEntityDTO(nodeID, displayName string, commoditiesSold []*proto.CommodityDTO) (*proto.EntityDTO, error) {
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

	metaData := generateReconciliationMetaData()
	entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

	entityDTOBuilder = entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for node %s: %s", nodeID, err)
	}

	return entityDto, nil
}

// Discover pods running on nodes specified in task.
func (worker *k8sDiscoveryWorker) findPodsRunningInTaskNodes() []api.Pod {
	var podList []api.Pod
	for _, node := range worker.task.NodeList() {
		nodeNonTerminatedPodsList, err := worker.findNonTerminatedPods(node.Name)
		if err != nil {
			glog.Errorf("Failed to find non-ternimated pods in %s", node.Name)
			continue
		}
		podList = append(podList, nodeNonTerminatedPodsList.Items...)
	}
	return podList
}

func (worker *k8sDiscoveryWorker) findNonTerminatedPods(nodeName string) (*api.PodList, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase!=" +
		string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	if err != nil {
		return nil, err
	}
	return worker.kubeClient.Pods(api.NamespaceAll).List(api.ListOptions{FieldSelector: fieldSelector})
}

