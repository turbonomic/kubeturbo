package worker

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

const (
	containerFoo  = "containerFoo"
	containerBar  = "containerBar"
	controllerUID = "controllerUID"

	containerFooCPUCap   = 5.0
	containerFooCPUUsed1 = 2.0
	containerFooCPUUsed2 = 4.0
	containerFooMemCap   = 400.0
	containerFooMemUsed1 = 200.0
	containerFooMemUsed2 = 300.0

	containerBarCPURequestCap   = 4.0
	containerBarCPURequestUsed1 = 1.0
	containerBarCPURequestUsed2 = 2.0
	containerBarMemRequestCap   = 400.0
	containerBarMemRequestUsed1 = 100.0
	containerBarMemRequestUsed2 = 200.0
)

var (
	testPod4 = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod",
			UID:       "pod-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(util.KindDeployment, "controller", controllerUID),
			},
		},
		Spec: api.PodSpec{
			NodeName: "node",
			Containers: []api.Container{
				{
					Name: containerFoo,
				},
			},
		},
	}
	testPod5 = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod",
			UID:       "pod-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(util.KindDeployment, "controller", controllerUID),
			},
		},
		Spec: api.PodSpec{
			NodeName: "node",
			Containers: []api.Container{
				{
					Name: containerBar,
					Resources: api.ResourceRequirements{
						Requests: buildResource(containerBarCPURequestCap, int64(containerBarCPURequestCap)),
					},
				},
			},
		},
	}

	ownerUIDMetric      = metrics.NewEntityStateMetric(metrics.PodType, discoveryutil.PodKeyFunc(testPod4), metrics.OwnerUID, controllerUID)
	podNodeCPUFrequency = metrics.NewEntityStateMetric(metrics.NodeType, discoveryutil.NodeKeyFromPodFunc(testPod4), metrics.CpuFrequency, cpuFrequency)

	containerFooCPUCapMetric = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.CPU, metrics.Capacity, containerFooCPUCap)
	containerFooCPUUsedMetric1 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.CPU, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerFooCPUUsed1, 1),
		})
	containerFooCPUUsedMetric2 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.CPU, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerFooCPUUsed2, 2),
		})
	containerFooMemCapMetric = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.Memory, metrics.Capacity, containerFooMemCap)
	containerFooMemUsedMetric1 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.Memory, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerFooMemUsed1, 1),
		})
	containerFooMemUsedMetric2 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerFoo), metrics.Memory, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerFooMemUsed2, 2),
		})

	containerBarCPURequestCapMetric = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.CPURequest, metrics.Capacity, containerBarCPURequestCap)
	containerBarCPURequestUsedMetric1 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.CPURequest, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerBarCPURequestUsed1, 1),
		})
	containerBarCPURequestUsedMetric2 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.CPURequest, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerBarCPURequestUsed2, 2),
		})
	containerBarMemRequestCapMetric = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.MemoryRequest, metrics.Capacity, containerBarMemRequestCap)
	containerBarMemRequestUsedMetric1 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.MemoryRequest, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerBarMemRequestUsed1, 1),
		})
	containerBarMemRequestUsedMetric2 = metrics.NewEntityResourceMetric(metrics.ContainerType,
		discoveryutil.ContainerMetricId(discoveryutil.PodMetricIdAPI(testPod4), containerBar), metrics.MemoryRequest, metrics.Used,
		[]metrics.Point{
			createContainerMetricPoint(containerBarMemRequestUsed2, 2),
		})
)

func TestContainerSpecMetricsCollector_CollectContainerSpecMetrics_WithoutRequestMetrics(t *testing.T) {
	pods := []*api.Pod{testPod4}
	metricsSink := metrics.NewEntityMetricSink().WithMaxMetricPointsSize(10)
	// Add pod owner UID and node CPU frequency metrics
	metricsSink.AddNewMetricEntries(ownerUIDMetric, podNodeCPUFrequency)

	// Add and update containerFoo CPU and memory metrics
	metricsSink.AddNewMetricEntries(containerFooCPUCapMetric, containerFooCPUUsedMetric1, containerFooMemCapMetric, containerFooMemUsedMetric1)
	metricsSink.UpdateMetricEntry(containerFooCPUUsedMetric2)
	metricsSink.UpdateMetricEntry(containerFooMemUsedMetric2)

	containerSpecMetricsCollector := NewContainerSpecMetricsCollector(metricsSink, pods)
	containerSpecMetricsList, _ := containerSpecMetricsCollector.CollectContainerSpecMetrics()

	expectedContainerSpecMetricsFoo := &repository.ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerFoo,
		ContainerSpecId:   discoveryutil.ContainerSpecIdFunc(controllerUID, containerFoo),
		ContainerReplicas: 1,
		ContainerMetrics: map[metrics.ResourceType]*repository.ContainerMetrics{
			metrics.CPU: {
				Capacity: cpuFrequency * containerFooCPUCap,
				Used: []metrics.Point{
					createContainerMetricPoint(cpuFrequency*containerFooCPUUsed1, 1),
					createContainerMetricPoint(cpuFrequency*containerFooCPUUsed2, 2),
				},
			},
			metrics.Memory: {
				Capacity: containerFooMemCap,
				Used: []metrics.Point{
					createContainerMetricPoint(containerFooMemUsed1, 1),
					createContainerMetricPoint(containerFooMemUsed2, 2),
				},
			},
		},
	}
	assert.EqualValues(t, 1, len(containerSpecMetricsList))
	assert.Equal(t, containerFoo, containerSpecMetricsList[0].ContainerSpecName)
	if !reflect.DeepEqual(expectedContainerSpecMetricsFoo, containerSpecMetricsList[0]) {
		t.Errorf("Test case failed: CollectContainerSpecMetrics():\nexpected:\n%++v\nactual:\n%++v", expectedContainerSpecMetricsFoo, containerSpecMetricsList[0])
	}
}

func TestContainerSpecMetricsCollector_CollectContainerSpecMetrics_WithRequestMetrics(t *testing.T) {
	pods := []*api.Pod{testPod5}
	metricsSink := metrics.NewEntityMetricSink().WithMaxMetricPointsSize(10)
	// Add pod owner UID and node CPU frequency metrics
	metricsSink.AddNewMetricEntries(ownerUIDMetric, podNodeCPUFrequency)

	// Add and update containerFoo CPURequest and MemoryRequest metrics
	metricsSink.AddNewMetricEntries(containerBarCPURequestCapMetric, containerBarCPURequestUsedMetric1, containerBarMemRequestCapMetric, containerBarMemRequestUsedMetric1)
	metricsSink.UpdateMetricEntry(containerBarCPURequestUsedMetric2)
	metricsSink.UpdateMetricEntry(containerBarMemRequestUsedMetric2)

	containerSpecMetricsCollector := NewContainerSpecMetricsCollector(metricsSink, pods)
	containerSpecMetricsList, _ := containerSpecMetricsCollector.CollectContainerSpecMetrics()

	expectedContainerSpecMetricsBar := &repository.ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerBar,
		ContainerSpecId:   discoveryutil.ContainerSpecIdFunc(controllerUID, containerBar),
		ContainerReplicas: 1,
		ContainerMetrics: map[metrics.ResourceType]*repository.ContainerMetrics{
			metrics.CPURequest: {
				Capacity: cpuFrequency * containerBarCPURequestCap,
				Used: []metrics.Point{
					createContainerMetricPoint(cpuFrequency*containerBarCPURequestUsed1, 1),
					createContainerMetricPoint(cpuFrequency*containerBarCPURequestUsed2, 2),
				},
			},
			metrics.MemoryRequest: {
				Capacity: containerBarMemRequestCap,
				Used: []metrics.Point{
					createContainerMetricPoint(containerBarMemRequestUsed1, 1),
					createContainerMetricPoint(containerBarMemRequestUsed2, 2),
				},
			},
		},
	}
	assert.EqualValues(t, 1, len(containerSpecMetricsList))
	assert.Equal(t, containerBar, containerSpecMetricsList[0].ContainerSpecName)
	if !reflect.DeepEqual(expectedContainerSpecMetricsBar, containerSpecMetricsList[0]) {
		t.Errorf("Test case failed: CollectContainerSpecMetrics():\nexpected:\n%++v\nactual:\n%++v", expectedContainerSpecMetricsBar, containerSpecMetricsList[0])
	}
}
