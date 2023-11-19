package worker

import (
	"fmt"

	api "k8s.io/api/core/v1"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
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

// ContainerSpecMetricsCollector collects list of ContainerSpecMetrics data from pods by the given discovery worker.
// Each ContainerSpecMetrics stores  resource capacity value and multiple usage data points from sampling discoveries
// for container replicas which belong to the same ContainerSpec.
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
				glog.V(3).Infof("Cannot find controller UID for pod %s/%s, %v", pod.Namespace, pod.Name, err)
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
				collector.collectContainerMetrics(containerSpecMetrics, containerMId, isCpuRequestSet, isMemRequestSet)

				containerSpecMetricsList = append(containerSpecMetricsList, containerSpecMetrics)
			}
		}
	}
	return containerSpecMetricsList
}

// collectContainerMetrics collects container metrics from metricsSink and stores them in the given ContainerSpecMetrics
func (collector *ContainerSpecMetricsCollector) collectContainerMetrics(containerSpecMetric *repository.ContainerSpecMetrics,
	containerMId string, isCpuRequestSet, isMemRequestSet bool) {
	for _, resourceType := range resourceTypes {
		if resourceType == metrics.VCPUThrottling && !utilfeature.DefaultFeatureGate.Enabled(features.ThrottlingMetrics) {
			// skip collecting throttling metrics if feature is disabled
			continue
		}
		if resourceType == metrics.CPURequest && !isCpuRequestSet || resourceType == metrics.MemoryRequest && !isMemRequestSet {
			// If CPU/Memory request is not set on container, no need to collect request resource metrics
			glog.V(4).Infof("Container %s has no %s set", containerMId, resourceType)
			continue
		}
		usedMetricPoints, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Used)
		if err != nil {
			if resourceType == metrics.VCPUThrottling {
				// We dont want to pollute the logs at low verbosity when we don't get throttling metrics from
				// kubelet. We add the throttling commodity even when we don't have the metrics.
				glog.V(5).Infof("Cannot get resource %s value for container %s %s: %v", metrics.Used, containerMId, resourceType, err)
				usedMetricPoints = [][]metrics.ThrottlingCumulative{}
			} else if utilfeature.DefaultFeatureGate.Enabled(features.KwokClusterTest) {
				// We simply put in a zero value on errors retriving a particular metrics
				usedMetricPoints = []metrics.Point{}
			} else {
				glog.Warningf("Cannot get resource %s value for container %s %s: %v", metrics.Used, containerMId, resourceType, err)
				continue
			}
		}
		var capVal float64
		if resourceType == metrics.VCPUThrottling {
			capVal = 100
		} else {
			capacityMetricValue, err := collector.getResourceMetricValue(containerMId, resourceType, metrics.Capacity)
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
	mType metrics.MetricProp) (interface{}, error) {
	metricUID := metrics.GenerateEntityResourceMetricUID(metrics.ContainerType, containerMId, rType, mType)
	resourceMetric, err := collector.metricsSink.GetMetric(metricUID)
	if err != nil {
		return nil, fmt.Errorf("missing metrics %s", metricUID)
	}
	switch typedValue := resourceMetric.GetValue().(type) {
	case []metrics.Point:
		// Create new metricPoints instead of modifying existing metricPoints values.
		// This will guarantee the data stored in metrics sink have original values when building container dtos.
		return util.CopyPoints(typedValue), nil
	case []metrics.Cumulative:
		return util.ConvertCumulativeToPoints(typedValue)
	case []metrics.ThrottlingCumulative:
		return util.CopyThrottlingCumulative(typedValue)
	case float64:
		metricValue := typedValue
		return metricValue, nil
	default:
		return nil, fmt.Errorf("unsupported metric value type: %v", resourceMetric.GetValue())
	}
}
