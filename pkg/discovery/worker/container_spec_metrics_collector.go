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
		metrics.VCPUThrottling,
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
func (collector *ContainerSpecMetricsCollector) CollectContainerSpecMetrics() []*repository.ContainerSpecMetrics {
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
	return containerSpecMetricsList
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
		usedMetricPoints, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Used, nodeCPUFrequency)
		if err != nil {
			glog.Warningf("Cannot get resource %s value for container %s %s: %v", metrics.Used, containerMId, resourceType, err)
			continue
		}
		var capVal float64
		if resourceType == metrics.VCPUThrottling {
			capVal = 100
		} else {
			capacityMetricValue, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Capacity, nodeCPUFrequency)
			if err != nil {
				glog.Warningf("Cannot get resource %s value for container %s %s", metrics.Capacity, containerMId, resourceType)
				continue
			}
			ok := false
			capVal, ok = capacityMetricValue.(float64)
			if !ok {
				glog.Errorf("Error getting resource %s value for container %s %s: capacityMetricValue is %t not 'float64' type",
					metrics.Capacity, containerMId, resourceType, capacityMetricValue)
				continue
			}
		}
		containerResourceMetrics := repository.NewContainerMetrics([]float64{capVal}, usedMetricPoints)
		containerSpecMetric.ContainerMetrics[resourceType] = containerResourceMetrics
	}
}

func (collector *ContainerSpecMetricsCollector) getResourceMetricValue(containerMId string, rType metrics.ResourceType,
	mType metrics.MetricProp, nodeCPUFrequency float64) (interface{}, error) {
	metricUID := metrics.GenerateEntityResourceMetricUID(metrics.ContainerType, containerMId, rType, mType)
	resourceMetric, err := collector.metricsSink.GetMetric(metricUID)
	if err != nil {
		return nil, fmt.Errorf("missing metrics %s", metricUID)
	}
	switch typedValue := resourceMetric.GetValue().(type) {
	case []metrics.Point:
		metricPoints := typedValue
		// Create new metricPoints instead of modifying existing metricPoints values if it's CPU type.
		// This will guarantee the data stored in metrics sink have original values when building container dtos.
		newMetricPoints := make([]metrics.Point, len(metricPoints))
		isCPUType := metrics.IsCPUType(rType)
		for i := range metricPoints {
			value := metricPoints[i].Value
			if isCPUType {
				// If resource is CPU type, convert values expressed in number of cores to MHz
				value *= nodeCPUFrequency
			}
			newMetricPoints[i] = metrics.Point{
				Value:     value,
				Timestamp: metricPoints[i].Timestamp,
			}
		}
		return newMetricPoints, nil
	case []metrics.ThrottlingCumulative:
		var newMetricTCs []metrics.ThrottlingCumulative
		numberOfSamples := len(typedValue)
		if typedValue != nil {
			newMetricTCs = make([]metrics.ThrottlingCumulative, numberOfSamples)
			copy(newMetricTCs, typedValue)
		}
		if numberOfSamples <= 1 {
			// we dont have enough samples.
			return [][]metrics.ThrottlingCumulative{}, fmt.Errorf("not enough data points [%d] to"+
				"aggregate throttling metrics for: %s", numberOfSamples, metricUID)
		}
		return [][]metrics.ThrottlingCumulative{newMetricTCs}, nil
	case float64:
		metricValue := typedValue
		if metrics.IsCPUType(rType) {
			// If resource is CPU type, convert metricValue expressed in number of cores to MHz
			metricValue *= nodeCPUFrequency
		}
		return metricValue, nil
	default:
		return nil, fmt.Errorf("unsupported metric value type: %t", resourceMetric.GetValue())
	}
}
