package dtofactory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	testCPUFrequency     = metrics.NewEntityStateMetric(metrics.NodeType, nodeName, metrics.CpuFrequency, nodeCpuFrequency)
	ownerUIDMetric       = metrics.NewEntityStateMetric(metrics.PodType, util.PodKeyFunc(testPod), metrics.OwnerUID, controllerUID)
	contFooSidecarMetric = metrics.NewEntityStateMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameFoo), metrics.IsInjectedSidecar, false)
	contSidecarSidecarMetric = metrics.NewEntityStateMetric(metrics.ContainerType,
		util.ContainerMetricId(util.PodMetricIdAPI(testPod), containerNameBar), metrics.IsInjectedSidecar, true)
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
	containerDTOs, _ := containerDTOBuilder.BuildEntityDTOs([]*api.Pod{testPod})

	assert.Equal(t, 2, len(containerDTOs))
	for _, containerDTO := range containerDTOs {
		if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerFoo) {
			assert.ElementsMatch(t, []*proto.ConnectedEntity{NewConnectedEntity(controllerUID, containerNameFoo)}, containerDTO.ConnectedEntities)
		} else if *containerDTO.DisplayName == util.ContainerNameFunc(testPod, &containerBar) {
			assert.ElementsMatch(t, containerDTO.ConnectedEntities, []*proto.ConnectedEntity{NewConnectedEntity(controllerUID, containerNameBar)})
		}
	}
}

func Test_containerDTOBuilder_BuildDTOs_sidecars(t *testing.T) {
	containerFoo := mockContainer(containerNameFoo)
	containerBar := mockContainer(containerNameBar)
	testPod.OwnerReferences = []metav1.OwnerReference{mockOwnerReference()}
	testPod.Spec.Containers = []api.Container{
		containerFoo,
		containerBar,
	}

	containerDTOBuilder := NewContainerDTOBuilder(mockMetricsSink())
	containerDTOs, sidecars := containerDTOBuilder.BuildEntityDTOs([]*api.Pod{testPod})

	assert.Equal(t, 1, len(sidecars))
	assert.Equal(t, 2, len(containerDTOs))
	sideCarSet := sets.NewString(sidecars...)
	assert.Equal(t, true, sideCarSet.Has(controllerUID+"/"+containerNameBar))
	assert.Equal(t, false, sideCarSet.Has(controllerUID+"/"+containerNameFoo))

}

func NewConnectedEntity(controllerUID string, containerName string) *proto.ConnectedEntity {
	entityID := util.ContainerSpecIdFunc(controllerUID, containerName)
	conType := proto.ConnectedEntity_CONTROLLED_BY_CONNECTION
	return &proto.ConnectedEntity{
		ConnectedEntityId: &entityID,
		ConnectionType:    &conType,
	}
}

func Test_containerDTOBuilder_BuildDTOs_containerData(t *testing.T) {
	containerFoo := mockContainer(containerNameFoo)
	containerFoo.Resources = api.ResourceRequirements{
		Limits: buildResource(cpuCap, int64(memCap)),
	}
	containerBar := mockContainer(containerNameBar)
	testPod.Spec.Containers = []api.Container{
		containerFoo,
		containerBar,
	}
	containerDTOBuilder := NewContainerDTOBuilder(mockMetricsSink())
	containerDTOs, _ := containerDTOBuilder.BuildEntityDTOs([]*api.Pod{testPod})
	assert.Equal(t, 2, len(containerDTOs))
	// containerFoo DTO has cpu and mem limits set.
	assert.True(t, containerDTOs[0].GetContainerData().GetHasCpuLimit())
	assert.True(t, containerDTOs[0].GetContainerData().GetHasMemLimit())
	// containerBar DTO doesn't have cpu and mem limits set.
	assert.False(t, containerDTOs[1].GetContainerData().GetHasCpuLimit())
	assert.False(t, containerDTOs[1].GetContainerData().GetHasMemLimit())
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
		containerBarCPUUsed, containerBarCPUCap, containerBarMemUsed, containerBarMemCap, testCPUFrequency,
		ownerUIDMetric, contFooSidecarMetric, contSidecarSidecarMetric)
	return metricsSink
}

func buildResource(cores float64, numMB int64) api.ResourceList {
	resourceList := make(api.ResourceList)
	resourceList[api.ResourceCPU] = getCPUQuantity(cores)
	resourceList[api.ResourceMemory] = getMemQuantity(numMB)
	return resourceList
}

func getCPUQuantity(cores float64) resource.Quantity {
	result, _ := resource.ParseQuantity(fmt.Sprintf("%dm", int(cores*1000)))
	return result
}

func getMemQuantity(numKB int64) resource.Quantity {
	result, _ := resource.ParseQuantity(fmt.Sprintf("%dMi", numKB))
	return result
}
