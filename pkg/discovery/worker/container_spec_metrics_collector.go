package worker

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

var (
	resourceTypes = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
	}
)

// Collect list of ContainerSpecMetrics data from pods by the given discovery worker. Each ContainerSpecMetrics stores
// resource capacity value and multiple usage data points from sampling discoveries for container replicas which belong
// to the same ContainerSpec.
type ContainerSpecMetricsCollector struct {
	podList     []*api.Pod
	metricsSink *metrics.EntityMetricSink
}

func NewContainerSpecMetricsCollector(metricsSink *metrics.EntityMetricSink, podList []*api.Pod) *ContainerSpecMetricsCollector {
	metricsCollector := &ContainerSpecMetricsCollector{
		podList:     podList,
		metricsSink: metricsSink,
	}
	return metricsCollector
}

// CollectContainerSpecMetrics collects list of ContainerSpecMetrics including resource capacity value and multiple
// usage data points from sampling discoveries for container replicas which belong to the same ContainerSpec.
func (collector *ContainerSpecMetricsCollector) CollectContainerSpecMetrics() ([]*repository.ContainerSpecMetrics, error) {
	var containerSpecMetricsList []*repository.ContainerSpecMetrics
	for _, pod := range collector.podList {
		// Create ContainerSpecMetrics only if Pod is deployed by a K8s controller
		if util.HasController(pod) {
			controllerUID, err := util.GetControllerUID(pod, collector.metricsSink)
			if err != nil {
				glog.Errorf("Error getting controller UID from pod %s, %v", pod.Name, err)
				continue
			}
			nodeCPUFrequency, err := util.GetNodeCPUFrequency(util.NodeKeyFromPodFunc(pod), collector.metricsSink)
			if err != nil {
				glog.Errorf("failed to build ContainerDTOs for pod[%s]: %v", pod.Name, err)
				continue
			}
			podMId := util.PodMetricIdAPI(pod)
			for _, container := range pod.Spec.Containers {
				// Create ContainerSpecMetrics object to collect resource metrics of each individual container replica for a
				// ContainerSpec entity. ContainerSpecMetrics entity includes resource capacity value and multiple resource
				// usage data points from sampling discoveries for a certain type of container replicas.
				containerSpecId := util.ContainerSpecIdFunc(controllerUID, container.Name)
				containerSpecMetrics := repository.NewContainerSpecMetrics(pod.Namespace, controllerUID, container.Name,
					containerSpecId)

				isCpuRequestSet := !container.Resources.Requests.Cpu().IsZero()
				isMemRequestSet := !container.Resources.Requests.Memory().IsZero()
				containerMId := util.ContainerMetricId(podMId, container.Name)
				collector.collectContainerMetrics(containerSpecMetrics, containerMId, nodeCPUFrequency, isCpuRequestSet, isMemRequestSet)

				containerSpecMetricsList = append(containerSpecMetricsList, containerSpecMetrics)
			}
		}
	}
	return containerSpecMetricsList, nil
}

// collectContainerMetrics collects container metrics from metricsSink and stores them in the given ContainerSpecMetrics
func (collector *ContainerSpecMetricsCollector) collectContainerMetrics(containerSpecMetric *repository.ContainerSpecMetrics,
	containerMId string, nodeCPUFrequency float64, isCpuRequestSet, isMemRequestSet bool) {
	for _, resourceType := range resourceTypes {
		if resourceType == metrics.CPURequest && !isCpuRequestSet || resourceType == metrics.MemoryRequest && !isMemRequestSet {
			// If CPU/Memory request is not set on container, no need to collect request resource metrics
			glog.V(4).Infof("Container %s has no %s set", containerMId, resourceType)
			continue
		}
		usedMetricValue, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Used, nodeCPUFrequency)
		if err != nil {
			glog.Errorf("Error getting resource %s value for container %s %s: %v", metrics.Used, containerMId, resourceType, err)
			continue
		}
		usedValPoints, ok := usedMetricValue.([]metrics.Point)
		if !ok {
			glog.Errorf("Error getting resource %s value for container %s %s: usedMetricValue is %t not '[]metrics.Point' type",
				metrics.Used, containerMId, resourceType, usedMetricValue)
			continue
		}
		capacityMetricValue, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Capacity, nodeCPUFrequency)
		if err != nil {
			glog.Errorf("Error getting resource %s value for container %s %s", metrics.Capacity, containerMId, resourceType)
			continue
		}
		capVal, ok := capacityMetricValue.(float64)
		if !ok {
			glog.Errorf("Error getting resource %s value for container %s %s: capacityMetricValue is %t not 'float64' type",
				metrics.Capacity, containerMId, resourceType, capacityMetricValue)
			continue
		}
		containerResourceMetrics := repository.NewContainerMetrics(capVal, usedValPoints)
		containerMetrics, exists := containerSpecMetric.ContainerMetrics[resourceType]
		if !exists {
			containerSpecMetric.ContainerMetrics[resourceType] = containerResourceMetrics
		} else {
			// Resource capacity of the same resource type is always the same for container replicas so no need to update.
			// Append resource used data points to containerMetrics.
			containerMetrics.Used = append(containerMetrics.Used, containerResourceMetrics.Used...)
		}
	}
}

func (collector *ContainerSpecMetricsCollector) getResourceMetricValue(containerMId string, rType metrics.ResourceType,
	mType metrics.MetricProp, nodeCPUFrequency float64) (interface{}, error) {
	metricUID := metrics.GenerateEntityResourceMetricUID(metrics.ContainerType, containerMId, rType, mType)
	resourceMetric, err := collector.metricsSink.GetMetric(metricUID)
	if err != nil {
		return nil, fmt.Errorf("missing metrics %s", metricUID)
	}
	switch resourceMetric.GetValue().(type) {
	case []metrics.Point:
		metricPoints, _ := resourceMetric.GetValue().([]metrics.Point)
		if metrics.IsCPUType(rType) {
			// If resource is CPU type, convert values expressed in number of cores to MHz
			for i := range metricPoints {
				metricPoints[i].Value *= nodeCPUFrequency
			}
		}
		return metricPoints, nil
	case float64:
		metricValue := resourceMetric.GetValue().(float64)
		if metrics.IsCPUType(rType) {
			// If resource is CPU type, convert metricValue expressed in number of cores to MHz
			metricValue *= nodeCPUFrequency
		}
		return metricValue, nil
	default:
		return nil, fmt.Errorf("unsupported metric value type: %t", resourceMetric.GetValue())
	}
}
