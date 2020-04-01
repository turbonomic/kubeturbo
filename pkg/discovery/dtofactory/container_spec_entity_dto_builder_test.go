package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"testing"
)

var (
	controllerUID = "controller-UID"
	namespace     = "namespace"
)

func TestGetControllerIdToPodmap(t *testing.T) {
	containerFoo1 := mockContainer("foo")
	containerBar1 := mockContainer("bar")
	pod1 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
			UID:       "pod1-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(),
			},
		},
		Spec: api.PodSpec{
			Containers: []api.Container{
				containerFoo1,
				containerBar1,
			},
		},
	}
	containerFoo2 := mockContainer("foo")
	containerBar2 := mockContainer("bar")
	pod2 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod2",
			UID:       "pod2-UID",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(),
			},
		},
		Spec: api.PodSpec{
			Containers: []api.Container{
				containerFoo2,
				containerBar2,
			},
		},
	}
	pods := []*api.Pod{pod1, pod2}
	ownerUIDMetric1 := metrics.NewEntityStateMetric(metrics.PodType, util.PodKeyFunc(pod1), metrics.OwnerUID, controllerUID)
	ownerUIDMetric2 := metrics.NewEntityStateMetric(metrics.PodType, util.PodKeyFunc(pod2), metrics.OwnerUID, controllerUID)

	containerSpecDTOBuilder := NewContainerSpecDTOBuilder(metrics.NewEntityMetricSink())
	containerSpecDTOBuilder.metricsSink.AddNewMetricEntries(ownerUIDMetric1, ownerUIDMetric2)

	controllerUIDToContainersMap := containerSpecDTOBuilder.getControllerUIDToContainersMap(pods)
	expectedControllerUIDToContainersMap := map[string]map[string]struct{}{
		controllerUID: {
			"foo": struct{}{},
			"bar": struct{}{},
		},
	}
	if !reflect.DeepEqual(expectedControllerUIDToContainersMap, controllerUIDToContainersMap) {
		t.Errorf("Test case failed: controllerIdToPodMap:\nexpected:\n%++v\nactual:\n%++v",
			expectedControllerUIDToContainersMap, controllerUIDToContainersMap)
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
