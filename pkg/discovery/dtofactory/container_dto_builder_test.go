package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	namespace        = "namespace"
	controllerUID    = "controller-UID"
	podName          = "pod"
	podUID           = "pod-UID"
	nodeName         = "node"
	containerNameFoo = "foo"
	containerNameBar = "bar"
	cpuUsed          = 1.0
	cpuCap           = 2.0
	memUsed          = 2.0
	memCap           = 3.0
	nodeCpuFrequency = 2048.0

	testPod = &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      podName,
			UID:       types.UID(podUID),
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
		},
	}

	containerFooCPUUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.CPU, metrics.Used, cpuUsed)
	containerFooCPUCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.CPU, metrics.Capacity, cpuCap)
	containerFooMemUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.Memory, metrics.Used, memUsed)
	containerFooMemCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.Memory, metrics.Capacity, memCap)
	containerBarCPUUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.CPU, metrics.Used, cpuUsed)
	containerBarCPUCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.CPU, metrics.Capacity, cpuCap)
	containerBarMemUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.Memory, metrics.Used, memUsed)
	containerBarMemCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.Memory, metrics.Capacity, memCap)
	testCPUFrequency = metrics.NewEntityStateMetric(metrics.NodeType, nodeName, metrics.CpuFrequency, nodeCpuFrequency)
	ownerUIDMetric   = metrics.NewEntityStateMetric(metrics.PodType, util.PodKeyFunc(testPod), metrics.OwnerUID, controllerUID)
)

func TestPodFlags(t *testing.T) {
	/*
	 * The following pods are in the canned topology:
	 *
	 * Pod 0: controllable = true, monitored = true, parentKind = ReplicationController
	 * (controllable should be true, monitored should be true)
	 *
	 * Pod 1: controllable = false, monitored = true, parentKind = DaemonSet
	 * (controllable should be true, monitored should be true)
	 *
	 * Pod 2: controllable = true, monitored = true, parentKind = ReplicaSet
	 * (controllable should be true, monitored should be true)
	 *
	 * Pod 3: controllable = false, monitored = true, parentKind = ReplicaSet
	 * This pod has the "kubeturbo.io/monitored" attribute set to false.
	 * (controllable should be true, monitored should be false)
	 */
	expectedResult := []struct {
		Controllable bool
		Monitored    bool
	}{
		{true, true},
		{true, true},
		{true, true},
		{false, true},
	}

	pods, err := LoadCannedTopology()
	if err != nil {
		t.Errorf("Cannot load test topology")
	}
	// Ensure that all pods loaded and parsed
	if len(pods) != 4 {
		t.Errorf("Could not load all 4 pods from test topology")
	}

	for i, pod := range pods {
		controllable := util.Controllable(pod)
		if controllable != expectedResult[i].Controllable {
			t.Errorf("Pod %d Controllable: expected %v, got %v", i,
				expectedResult[i].Controllable, controllable)
		}
	}
}

func Test_containerDTOBuilder_BuildDTOs_layeredOver(t *testing.T) {
	containerFoo := mockContainer(containerNameFoo)
	containerBar := mockContainer(containerNameBar)
	testPod.OwnerReferences = []metav1.OwnerReference{mockOwnerReference()}
	testPod.Spec.Containers = []api.Container{
		containerFoo,
		containerBar,
	}

	containerDTOBuilder := NewContainerDTOBuilder(mockMetricsSink())
	containerDTOs := containerDTOBuilder.BuildEntityDTOs([]*api.Pod{testPod})

	assert.Equal(t, 2, len(containerDTOs))
	for _, containerDTO := range containerDTOs {
		if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerFoo) {
			assert.ElementsMatch(t, []string{util.ContainerSpecIdFunc(controllerUID, containerNameFoo)}, containerDTO.LayeredOver)
		} else if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerBar) {
			assert.ElementsMatch(t, []string{util.ContainerSpecIdFunc(controllerUID, containerNameBar)}, containerDTO.LayeredOver)
		}
	}
}

func mockOwnerReference() (r metav1.OwnerReference) {
	isController := true
	return metav1.OwnerReference{
		Kind:       "Deployment",
		Name:       "api",
		UID:        types.UID(controllerUID),
		Controller: &isController,
	}
}

func mockContainer(name string) api.Container {
	container := api.Container{
		Name: name,
	}
	return container
}

func mockMetricsSink() *metrics.EntityMetricSink {
	metricsSink = metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(containerFooCPUUsed, containerFooMemUsed, containerFooCPUCap, containerFooMemCap,
		containerBarCPUUsed, containerBarCPUCap, containerBarMemUsed, containerBarMemCap, testCPUFrequency, ownerUIDMetric)
	return metricsSink
}
