package dtofactory

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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

	cpuCommType = proto.CommodityDTO_VCPU_MILICORE
	memCommType = proto.CommodityDTO_VMEM

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
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.CPUMili, metrics.Used, cpuUsed)
	containerFooCPUCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.CPUMili, metrics.Capacity, cpuCap)
	containerFooMemUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.Memory, metrics.Used, memUsed)
	containerFooMemCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.Memory, metrics.Capacity, memCap)
	containerBarCPUUsed = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.CPUMili, metrics.Used, cpuUsed)
	containerBarCPUCap = metrics.NewEntityResourceMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.CPUMili, metrics.Capacity, cpuCap)
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

func dumpPodFlags(pods []*api.Pod) {
	// This code dumps the attributes of the saved topology
	for i, pod := range pods {
		parentKind, _, _, _ := util.GetPodParentInfo(pod)
		fmt.Printf("Pod %d: controllable = %v, parentKind = %s\n", i,
			util.Controllable(pod),
			parentKind)
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
	containerDTOs, _, err := containerDTOBuilder.BuildDTOs([]*api.Pod{testPod})

	assert.Nil(t, err)
	assert.Equal(t, 2, len(containerDTOs))
	for _, containerDTO := range containerDTOs {
		if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerFoo) {
			assert.ElementsMatch(t, []string{util.ContainerSpecIdFunc(controllerUID, containerNameFoo)}, containerDTO.LayeredOver)
		} else if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerBar) {
			assert.ElementsMatch(t, []string{util.ContainerSpecIdFunc(controllerUID, containerNameBar)}, containerDTO.LayeredOver)
		}
	}
}

func Test_containerDTOBuilder_BuildDTOs_withContainerSpec(t *testing.T) {
	// Test Pod with OwnerReference (deployed by K8s controller)
	testPod.OwnerReferences = []metav1.OwnerReference{mockOwnerReference()}
	testPod.Spec.Containers = []api.Container{
		mockContainer(containerNameFoo),
	}

	containerDTOBuilder := NewContainerDTOBuilder(mockMetricsSink())
	_, containerSpecs, err := containerDTOBuilder.BuildDTOs([]*api.Pod{testPod})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(containerSpecs))

	cpuUsedFreq := cpuUsed
	cpuPeakFreq := cpuUsed
	cpuCapFreq := cpuCap
	commIsResizable := false
	expectedContainerSpec := &repository.ContainerSpec{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerNameFoo,
		ContainerSpecId:   "controller-UID/foo",
		ContainerReplicas: 1,
		ContainerCommodities: map[proto.CommodityDTO_CommodityType][]*proto.CommodityDTO{
			cpuCommType: {{
				CommodityType: &cpuCommType,
				Used:          &cpuUsedFreq,
				Peak:          &cpuPeakFreq,
				Capacity:      &cpuCapFreq,
				Resizable:     &commIsResizable,
			},
			},
			memCommType: {{
				CommodityType: &memCommType,
				Used:          &memUsed,
				Peak:          &memUsed,
				Capacity:      &memCap,
				Resizable:     &commIsResizable,
			},
			},
		},
	}
	if !reflect.DeepEqual(expectedContainerSpec, containerSpecs[0]) {
		t.Errorf("Test case failed: BuildDTOs_withoutContainerSpec:\nexpected:\n%++v\nactual:\n%++v",
			expectedContainerSpec, containerSpecs[0])
	}
}

func Test_containerDTOBuilder_BuildDTOs_withoutContainerSpec(t *testing.T) {
	// Test Pod with nil OwnerReference (bare pod deployed without K8s controller)
	testPod.OwnerReferences = nil
	testPod.Spec.Containers = []api.Container{
		mockContainer(containerNameFoo),
	}

	containerDTOBuilder := NewContainerDTOBuilder(mockMetricsSink())

	_, containerSpecs, err := containerDTOBuilder.BuildDTOs([]*api.Pod{testPod})
	assert.Nil(t, err)
	assert.Equal(t, 0, len(containerSpecs))
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
