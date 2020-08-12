package worker

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

// Collect K8s controller info from the pods by the given discovery worker and convert to KubeController objects.
type ControllerMetricsCollector struct {
	podList     []*api.Pod
	cluster     *repository.ClusterSummary
	metricsSink *metrics.EntityMetricSink
	workerId    string
}

func NewControllerMetricsCollector(discoveryWorker *k8sDiscoveryWorker, currTask *task.Task) *ControllerMetricsCollector {
	metricsCollector := &ControllerMetricsCollector{
		podList:     currTask.PodList(),
		cluster:     currTask.Cluster(),
		metricsSink: discoveryWorker.sink,
		workerId:    discoveryWorker.id,
	}
	return metricsCollector
}

// Collect allocation resource metrics for K8s controllers where usage values are aggregated from pods and capacity values
// are from namespace quota capacity.
func (collector *ControllerMetricsCollector) CollectControllerMetrics() ([]*repository.KubeController, error) {
	if collector.cluster == nil {
		return nil, fmt.Errorf("error collecting K8s controller metrics, because cluster summary object is null for discovery worker %s",
			collector.workerId)
	}
	kubeNamespaceMap := collector.cluster.NamespaceMap
	// Map from controller UID to the corresponding kubeController
	kubeControllersMap := make(map[string]*repository.KubeController)
	var kubeControllerList []*repository.KubeController
	for _, pod := range collector.podList {
		if !util.HasController(pod) {
			// If pod has no Controller, it is a bare pod directly deployed on Namespace. Skip this.
			glog.V(4).Infof("Skip creating KubeController for bare Pod %s", util.PodKeyFunc(pod))
			continue
		}
		controllerType, controllerName, controllerUID, err := collector.getKubeControllerInfo(metrics.PodType, util.PodKeyFunc(pod))
		if err != nil {
			glog.Errorf("Error getting controller info from Pod %s: %v", util.PodKeyFunc(pod), err)
			continue
		}
		kubeController, exists := kubeControllersMap[controllerUID]
		if !exists {
			namespace := pod.Namespace
			// Create default KubeController entity
			kubeController = repository.NewKubeController(collector.cluster.Name, namespace, controllerName, controllerType,
				controllerUID)
			kubeNamespace, namespaceExists := kubeNamespaceMap[namespace]
			if !namespaceExists {
				glog.Errorf("Namespace %s does not exist in cluster %s", namespace, collector.cluster.Name)
				continue
			}
			// Add quota resources to KubeController entity with capacity as namespace quota capacity
			for _, resourceType := range metrics.QuotaResources {
				namespaceQuotaResource, err := kubeNamespace.GetAllocationResource(resourceType)
				resourceCapacity := repository.DEFAULT_METRIC_CAPACITY_VALUE
				if err != nil {
					glog.Errorf("KubeNamespace %s has invalid resource %s. Set %s capacity of k8s controller %s to %v",
						kubeNamespace.Name, resourceType, resourceType, kubeController.GetFullName(), repository.DEFAULT_METRIC_CAPACITY_VALUE)
				} else {
					// For CPU resources, set capacity values in number of cores here. Will update CPU resources capacity
					// to MHz based on average node CPU frequency when building workload controller entity DTO in
					// controller_discovery_worker. Each k8s controller metrics collector collects controller metrics
					// from certain amount of nodes run by a k8s discovery worker. So at this point, there's no way to
					// calculate average CPU frequency of all nodes in the cluster.
					resourceCapacity = namespaceQuotaResource.Capacity
				}
				kubeController.AddAllocationResource(resourceType, resourceCapacity, repository.DEFAULT_METRIC_VALUE)
			}
			kubeControllersMap[controllerUID] = kubeController
			kubeControllerList = append(kubeControllerList, kubeController)
		}
		kubeController.Pods = append(kubeController.Pods, pod)
		// Update quota resources usage for the controller aggregated from pods quota usage
		collector.updateQuotaResourcesUsed(kubeController, pod)
	}
	return kubeControllerList, nil
}

// Get KubeController info from the given pod from metrics sink, including controller type, name and UID.
func (collector *ControllerMetricsCollector) getKubeControllerInfo(entityType metrics.DiscoveredEntityType,
	entityKey string) (string, string, string, error) {
	controllerType, err := collector.getOwnerMetric(entityType, entityKey, metrics.OwnerType)
	if err != nil {
		return "", "", "", err
	}
	controllerName, err := collector.getOwnerMetric(entityType, entityKey, metrics.Owner)
	if err != nil {
		return "", "", "", err
	}
	controllerUID, err := collector.getOwnerMetric(entityType, entityKey, metrics.OwnerUID)
	if err != nil {
		return "", "", "", err
	}
	return controllerType, controllerName, controllerUID, nil
}

// Get Pod owner metric value of a given metric type from metrics sink. Metric type can be Owner (owner name), OwnerType
// or OwnerUID.
func (collector *ControllerMetricsCollector) getOwnerMetric(entityType metrics.DiscoveredEntityType, entityKey string,
	ownerMetricType metrics.ResourceType) (string, error) {
	ownerMetricId := metrics.GenerateEntityStateMetricUID(entityType, entityKey, ownerMetricType)
	ownerMetric, err := collector.metricsSink.GetMetric(ownerMetricId)
	if err != nil {
		return "", fmt.Errorf("error getting %s from metrics sink for pod %s --> %v", ownerMetricType, entityKey, err)
	}
	ownerMetricValue := ownerMetric.GetValue()
	metricValue, ok := ownerMetricValue.(string)
	if !ok {
		return "", fmt.Errorf("error getting %s from metrics sink for pod %s", ownerMetricType, entityKey)
	}
	return metricValue, nil
}

// Update quota resources used for the controller from pod quota usage. The usage values of CPU resources are converted
// from cores to MHz based on the CPU frequency of the corresponding node where the given pod is located.
func (collector *ControllerMetricsCollector) updateQuotaResourcesUsed(kubeController *repository.KubeController, pod *api.Pod) {
	for _, resourceType := range metrics.QuotaResources {
		podKey := util.PodKeyFunc(pod)
		metricId := metrics.GenerateEntityResourceMetricUID(metrics.PodType, podKey, resourceType, metrics.Used)
		metric, err := collector.metricsSink.GetMetric(metricId)
		if err != nil {
			glog.Errorf("Error getting %s used value from metrics sink for pod %s: %v", resourceType, podKey, err)
			continue
		}
		resourceUsed := metric.GetValue().(float64)
		if metrics.IsCPUType(resourceType) {
			// For CPU resources, convert the usage values expressed in cores to MHz based on the CPU frequency of the
			// corresponding node where the given pod is located
			cpuFrequency, err := util.GetNodeCPUFrequency(util.NodeKeyFromPodFunc(pod), collector.metricsSink)
			if err != nil {
				glog.Errorf("Error getting node cpuFrequency from pod %s: %v", podKey, err)
				continue
			}
			if cpuFrequency > 0.0 {
				usedValue := resourceUsed * cpuFrequency
				if usedValue > 0.0 {
					glog.V(4).Infof("Changing usage of %s::%s from pod %s from %f cores to %f MHz",
						kubeController.GetFullName(), resourceType, pod.Name, resourceUsed, usedValue)
				}
				resourceUsed = usedValue
			}
		}
		// Get existing resource of the given resourceType where used value is to be updated
		existingResource, err := kubeController.GetResource(resourceType)
		if err != nil {
			glog.Errorf("Error getting resource %s from controller %s", resourceType, kubeController.GetFullName())
			continue
		}
		existingResource.Used += resourceUsed
	}
}
