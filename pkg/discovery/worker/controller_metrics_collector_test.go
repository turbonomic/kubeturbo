package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	namespace      = "namespace"
	controller1    = "controller1"
	controllerUID1 = "controllerUID1"
	controller2    = "controller2"
	controllerUID2 = "controllerUID2"

	// testPod1 and testPod2 are from the same Deployment controller
	testPod1 = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
			UID:       "pod1-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(util.KindDeployment, controller1, controllerUID1),
			},
		},
		Spec: api.PodSpec{
			NodeName: "node1",
		},
	}
	testPod2 = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod2",
			UID:       "pod2-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(util.KindDeployment, controller1, controllerUID1),
			},
		},
		Spec: api.PodSpec{
			NodeName: "node2",
		},
	}
	// pod3 is from StatefulSet controller
	testPod3 = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod3",
			UID:       "pod3-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(util.KindStatefulSet, controller2, controllerUID2),
			},
		},
		Spec: api.PodSpec{
			NodeName: "node1",
		},
	}

	// Create owner metrics for testPod1, testPod2 and testPod3, including owner kind, name and uid
	pod1OwnerMetrics = createOwnerMetrics(testPod1, util.KindDeployment, controller1, controllerUID1)
	pod2OwnerMetrics = createOwnerMetrics(testPod2, util.KindDeployment, controller1, controllerUID1)
	pod3OwnerMetrics = createOwnerMetrics(testPod3, util.KindStatefulSet, controller2, controllerUID2)

	pod1CPULimitQuotaUsed      = metrics.NewEntityResourceMetric(metrics.PodType, discoveryutil.PodKeyFunc(testPod1), metrics.CPULimitQuota, metrics.Used, 1.0)
	pod2CPULimitQuotaUsed      = metrics.NewEntityResourceMetric(metrics.PodType, discoveryutil.PodKeyFunc(testPod2), metrics.CPULimitQuota, metrics.Used, 2.0)
	pod3MemoryRequestQuotaUsed = metrics.NewEntityResourceMetric(metrics.PodType, discoveryutil.PodKeyFunc(testPod3), metrics.MemoryRequestQuota, metrics.Used, 10.0)
)

func TestCollectControllerMetrics(t *testing.T) {
	kubeNamespace := repository.CreateDefaultKubeNamespace(cluster1, namespace, "namespace-uuid")
	_ = kubeNamespace.SetResourceCapacity(metrics.CPULimitQuota, 4.0)
	_ = kubeNamespace.SetResourceCapacity(metrics.MemoryLimitQuota, 3.0)

	kubeCluster := &repository.KubeCluster{
		Name: "cluster",
		NamespaceMap: map[string]*repository.KubeNamespace{
			namespace: kubeNamespace,
		},
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	pods := []*api.Pod{testPod1, testPod2, testPod3}
	currTask := task.NewTask().WithPods(pods).WithCluster(clusterSummary)
	workerConfig := NewK8sDiscoveryWorkerConfig("sType", 10, 10).
		WithMonitoringWorkerConfig(kubelet.NewKubeletMonitorConfig(nil, nil))
	discoveryWorker, _ := NewK8sDiscoveryWorker(workerConfig, "wid", metrics.NewEntityMetricSink(), true)

	// Add owner metrics to the metric sink
	var ownerMetricsList []metrics.EntityStateMetric
	ownerMetricsList = append(ownerMetricsList, pod1OwnerMetrics...)
	ownerMetricsList = append(ownerMetricsList, pod2OwnerMetrics...)
	ownerMetricsList = append(ownerMetricsList, pod3OwnerMetrics...)
	for _, ownerMetrics := range ownerMetricsList {
		discoveryWorker.sink.AddNewMetricEntries(ownerMetrics)
	}

	// Add resource usage metrics to the metric sink
	discoveryWorker.sink.AddNewMetricEntries(pod1CPULimitQuotaUsed)
	discoveryWorker.sink.AddNewMetricEntries(pod2CPULimitQuotaUsed)
	discoveryWorker.sink.AddNewMetricEntries(pod3MemoryRequestQuotaUsed)

	// Test CollectControllerMetrics
	controllerMetricsCollector := NewControllerMetricsCollector(discoveryWorker, currTask)
	kubeControllers := controllerMetricsCollector.CollectControllerMetrics()

	// 2 KubeControllers are created
	assert.Equal(t, 2, len(kubeControllers))

	// CPULimitQuota usage of the first controller is aggregated from testPod1 and testPod2.
	kubeController1 := kubeControllers[0]
	assert.Equal(t, util.KindDeployment, kubeController1.ControllerType)
	assert.Equal(t, 2, len(kubeController1.Pods))
	kubeController1AllocationResources := kubeController1.AllocationResources
	assert.Equal(t, 4, int(kubeController1AllocationResources[metrics.CPULimitQuota].Capacity))
	// Usage value is aggregated from testPod1 and testPod2:
	// (1.0 + 2.0) = 3
	assert.Equal(t, 3, int(kubeController1AllocationResources[metrics.CPULimitQuota].Used))
	assert.Equal(t, 4, int(kubeController1AllocationResources[metrics.CPULimitQuota].Capacity))
	assert.Equal(t, 3, int(kubeController1AllocationResources[metrics.MemoryLimitQuota].Capacity))
	// Resource capacity of metrics.CPURequestQuota and metrics.MemoryRequestQuota is not configured on namespace,
	// by default capacity is repository.DEFAULT_METRIC_CAPACITY_VALUE
	assert.Equal(t, repository.DEFAULT_METRIC_CAPACITY_VALUE, kubeController1AllocationResources[metrics.CPURequestQuota].Capacity)
	assert.Equal(t, repository.DEFAULT_METRIC_CAPACITY_VALUE, kubeController1AllocationResources[metrics.MemoryRequestQuota].Capacity)

	kubeController2 := kubeControllers[1]
	assert.Equal(t, util.KindStatefulSet, kubeController2.ControllerType)
	assert.Equal(t, 1, len(kubeController2.Pods))
	kubeController2AllocationResources := kubeController2.AllocationResources
	assert.Equal(t, 10, int(kubeController2AllocationResources[metrics.MemoryRequestQuota].Used))
	assert.Equal(t, 4, int(kubeController2AllocationResources[metrics.CPULimitQuota].Capacity))
	assert.Equal(t, 3, int(kubeController2AllocationResources[metrics.MemoryLimitQuota].Capacity))
	// Resource capacity of metrics.CPURequestQuota and metrics.MemoryRequestQuota is not configured on namespace,
	// by default capacity is repository.DEFAULT_METRIC_CAPACITY_VALUE
	assert.Equal(t, repository.DEFAULT_METRIC_CAPACITY_VALUE, kubeController2AllocationResources[metrics.CPURequestQuota].Capacity)
	assert.Equal(t, repository.DEFAULT_METRIC_CAPACITY_VALUE, kubeController2AllocationResources[metrics.MemoryRequestQuota].Capacity)
}

func mockOwnerReference(kind, name, uid string) metav1.OwnerReference {
	isController := true
	return metav1.OwnerReference{
		Kind:       kind,
		Name:       name,
		UID:        types.UID(uid),
		Controller: &isController,
	}
}

func createOwnerMetrics(pod *api.Pod, kind, name, uid string) []metrics.EntityStateMetric {
	ownerTypeMetric := metrics.NewEntityStateMetric(metrics.PodType, discoveryutil.PodKeyFunc(pod), metrics.OwnerType, kind)
	ownerMetric := metrics.NewEntityStateMetric(metrics.PodType, discoveryutil.PodKeyFunc(pod), metrics.Owner, name)
	ownerUIDMetric := metrics.NewEntityStateMetric(metrics.PodType, discoveryutil.PodKeyFunc(pod), metrics.OwnerUID, uid)
	return []metrics.EntityStateMetric{ownerTypeMetric, ownerMetric, ownerUIDMetric}
}
