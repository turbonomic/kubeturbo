package worker

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	agg "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
)

func Test_k8sContainerSpecDiscoveryWorker_getContainerDataAggregator(t *testing.T) {
	worker := &k8sContainerSpecDiscoveryWorker{}
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators("allUtilizationData", "maxUsageData")
	assert.Equal(t, agg.ContainerUtilizationDataAggregators["allUtilizationData"], utilizationDataAggregator)
	assert.Equal(t, agg.ContainerUsageDataAggregators["maxUsageData"], usageDataAggregator)
}

func Test_k8sContainerSpecDiscoveryWorker_getContainerDataAggregator_defaultStrategy(t *testing.T) {
	worker := &k8sContainerSpecDiscoveryWorker{}
	utilizationDataAggregator, usageDataAggregator := worker.getContainerDataAggregators("testUtilizationDataStrategy", "testUsageDataStrategy")
	// If input utilizationDataAggStrategy and usageDataAggStrategy are not support, use default data aggregators
	assert.Equal(t, agg.ContainerUtilizationDataAggregators[agg.DefaultContainerUtilizationDataAggStrategy], utilizationDataAggregator)
	assert.Equal(t, agg.ContainerUsageDataAggregators[agg.DefaultContainerUsageDataAggStrategy], usageDataAggregator)
}

func Test_k8sContainerSpecDiscoveryWorker_createContainerSpecMetricsMap(t *testing.T) {
	namespace := "namespace"
	controllerUID := "controllerUID"
	containerSpecName := "containerSpecName"
	containerSpecId := "containerSpecId"
	cpuResourceType := metrics.CPU
	memResourceType := metrics.Memory

	// containerSpecMetrics1 and containerSpecMetrics2 collect metrics of the same ContainerSpec entity from 2 container replicas
	containerSpecMetrics1 := &repository.ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 1,
		ContainerMetrics: map[metrics.ResourceType]*repository.ContainerMetrics{
			cpuResourceType: {
				Capacity: []float64{2.0},
				Used: []metrics.Point{
					createContainerMetricPoint(1.0, 1),
					createContainerMetricPoint(1.0, 2),
				},
			},
			memResourceType: {
				Capacity: []float64{2.0},
				Used: []metrics.Point{
					createContainerMetricPoint(1.0, 1),
					createContainerMetricPoint(1.0, 2),
				},
			},
		},
	}
	containerSpecMetrics2 := &repository.ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 1,
		ContainerMetrics: map[metrics.ResourceType]*repository.ContainerMetrics{
			cpuResourceType: {
				Capacity: []float64{3.0},
				Used: []metrics.Point{
					createContainerMetricPoint(2.0, 1),
					createContainerMetricPoint(2.0, 2),
				},
			},
			memResourceType: {
				Capacity: []float64{3.0},
				Used: []metrics.Point{
					createContainerMetricPoint(2.0, 1),
					createContainerMetricPoint(2.0, 2),
				},
			},
		},
	}

	expectedContainerSpec := &repository.ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerSpecName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 2,
		ContainerMetrics: map[metrics.ResourceType]*repository.ContainerMetrics{
			cpuResourceType: {
				Capacity: []float64{2.0, 3.0},
				Used: []metrics.Point{
					createContainerMetricPoint(1.0, 1),
					createContainerMetricPoint(1.0, 2),
					createContainerMetricPoint(2.0, 1),
					createContainerMetricPoint(2.0, 2),
				},
			},
			memResourceType: {
				Capacity: []float64{2.0, 3.0},
				Used: []metrics.Point{
					createContainerMetricPoint(1.0, 1),
					createContainerMetricPoint(1.0, 2),
					createContainerMetricPoint(2.0, 1),
					createContainerMetricPoint(2.0, 2),
				},
			},
		},
	}

	worker := &k8sContainerSpecDiscoveryWorker{}
	containerSpecMetricsMap := worker.createContainerSpecMetricsMap([]*repository.ContainerSpecMetrics{containerSpecMetrics1, containerSpecMetrics2})
	containerSpecMetrics := containerSpecMetricsMap[containerSpecId]
	if !reflect.DeepEqual(expectedContainerSpec, containerSpecMetrics) {
		t.Errorf("Test case failed: createContainerSpecMap:\nexpected:\n%++v\nactual:\n%++v",
			expectedContainerSpec, containerSpecMetrics)
	}
}

func createContainerMetricPoint(value float64, timestamp int64) metrics.Point {
	return metrics.Point{
		Value:     value,
		Timestamp: timestamp,
	}
}
