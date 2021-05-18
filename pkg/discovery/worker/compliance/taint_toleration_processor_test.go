package compliance

import (
	"fmt"
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	t1    = newTaint("k1", "v1", api.TaintEffectNoExecute)
	t2    = newTaint("k2", "v2", api.TaintEffectNoSchedule)
	t3    = newTaint("k3", "v3", api.TaintEffectPreferNoSchedule)
	key1  = "k1=v1:NoExecute"
	key2  = "k2=v2:NoSchedule"
	n1    = newNodeWithTaints("node-1", []api.Taint{t1})
	n2    = newNodeWithTaints("node-2", []api.Taint{t2})
	n3    = newNodeWithTaints("node-3", []api.Taint{t3})
	nodes = map[string]*api.Node{
		string(n1.UID): n1,
		string(n2.UID): n2,
		string(n3.UID): n3,
	}

	tole1 = newToleration("k1", "v1", api.TaintEffectNoExecute, api.TolerationOpEqual)
	tole2 = newToleration("k2", "v2", api.TaintEffectNoSchedule, api.TolerationOpEqual)

	pod1 = newPodWithTolerations("pod-1", "node-1", []api.Toleration{})
	pod2 = newPodWithTolerations("pod-2", "node-2", []api.Toleration{tole1})
	pod3 = newPodWithTolerations("pod-3", "node-3", []api.Toleration{tole1, tole2})

	podDTO1 = newEntityDTO("pod-1", proto.EntityDTO_CONTAINER_POD, createCommBoughtForPod("node-1"))
	podDTO2 = newEntityDTO("pod-2", proto.EntityDTO_CONTAINER_POD, createCommBoughtForPod("node-2"))
	podDTO3 = newEntityDTO("pod-3", proto.EntityDTO_CONTAINER_POD, createCommBoughtForPod("node-3"))

	nodeDTO1 = newEntityDTO("node-1", proto.EntityDTO_VIRTUAL_MACHINE, []*proto.EntityDTO_CommodityBought{})
	nodeDTO2 = newEntityDTO("node-2", proto.EntityDTO_VIRTUAL_MACHINE, []*proto.EntityDTO_CommodityBought{})
	nodeDTO3 = newEntityDTO("node-3", proto.EntityDTO_VIRTUAL_MACHINE, []*proto.EntityDTO_CommodityBought{})

	otherDTO1 = newEntityDTO("foo-1", proto.EntityDTO_PHYSICAL_MACHINE, []*proto.EntityDTO_CommodityBought{})
)

func TestProcess(t *testing.T) {
	nodeAndPodGetter := &mockNodeAndPodGetter{
		nodes: []*api.Node{n1, n2, n3},
		pods:  []*api.Pod{pod1, pod2, pod3},
	}

	clusterName := "Test"
	kubeCluster := repository.NewKubeCluster(clusterName, nodeAndPodGetter.nodes)

	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	clusterSummary.NodeToRunningPods[n1.Name] = []*api.Pod{pod1}
	clusterSummary.NodeToRunningPods[n2.Name] = []*api.Pod{pod2}
	clusterSummary.NodeToRunningPods[n3.Name] = []*api.Pod{pod3}

	taintTolerationProcessor, err := NewTaintTolerationProcessor(clusterSummary)

	if err != nil {
		t.Errorf("Failed to create TaintTolerationProcessor: %v", err)
		return
	}

	dto1 := *podDTO1
	dto2 := *podDTO2
	dto3 := *podDTO3
	dto4 := *nodeDTO1
	dto5 := *nodeDTO2
	dto6 := *nodeDTO3
	dto7 := *otherDTO1

	entityDTOs := []*proto.EntityDTO{&dto1, &dto2, &dto3, &dto4, &dto5, &dto6, &dto7}
	taintTolerationProcessor.Process(entityDTOs)

	// Check entity DTOs for access commodities created from tatins and tolerations
	checkPodEntity(t, &dto1, "node-1")
	checkPodEntity(t, &dto2, "node-2")
	checkPodEntity(t, &dto3, "node-3")

	checkNodeEntity(t, &dto4, 1)
	checkNodeEntity(t, &dto5, 1)
	checkNodeEntity(t, &dto6, 2)

}

func checkPodEntity(t *testing.T, dto1 *proto.EntityDTO, providerId string) {
	if len(dto1.CommoditiesBought) != 1 {
		t.Errorf("Expected 2 CommoditiesBought but got %d", len(dto1.CommoditiesBought))
		return
	}

	if *(dto1.CommoditiesBought[0].ProviderId) != providerId {
		t.Errorf("Wrong provider ID %s", *(dto1.CommoditiesBought[0].ProviderId))
	}
}

func checkNodeEntity(t *testing.T, dto1 *proto.EntityDTO, numComms int) {
	if len(dto1.CommoditiesSold) != numComms {
		t.Errorf("Expected %d Commodities sold but got %d", numComms, len(dto1.CommoditiesSold))
	}
}

type mockNodeAndPodGetter struct {
	nodes []*api.Node
	pods  []*api.Pod
}

func (m *mockNodeAndPodGetter) GetAllNodes() ([]*api.Node, error) {
	return m.nodes, nil
}
func (m *mockNodeAndPodGetter) GetAllPods() ([]*api.Pod, error) {
	return m.pods, nil
}

func TestGetTaintCollection(t *testing.T) {
	taintCollection := getTaintCollection(nodes)

	fmt.Printf("taintCollection: %++v", taintCollection)

	if len(taintCollection) != 2 {
		t.Errorf("Expected 2 taints but got %d", len(taintCollection))
	}

	if value, ok := taintCollection[t1]; !ok {
		t.Errorf("Taint %+v not found", t1)
	} else if value != key1 {
		t.Errorf("Taint %+v has wrong key %s", t1, value)
	}

	if value, ok := taintCollection[t2]; !ok {
		t.Errorf("Taint %+v not found", t2)
	} else if value != key2 {
		t.Errorf("Taint %+v has wrong key %s", t2, value)
	}
}

func TestCreateTaintAccessComms(t *testing.T) {
	taintCollection := getTaintCollection(nodes)

	comms, err := createTaintAccessComms(n1, taintCollection)

	if err != nil {
		t.Errorf("Error: %v", err)
	}

	if len(comms) != 1 {
		t.Errorf("Expected 1 commodity but got %d", len(comms))
	}

	if *(comms[0].Key) != key2 {
		t.Errorf("Comm %+v has wrong key %s", comms[0], *(comms[0].Key))
	}

	comms2, err := createTaintAccessComms(n2, taintCollection)

	if len(comms2) != 1 {
		t.Errorf("Expected 1 commodity but got %d", len(comms2))
	}

	if *(comms2[0].Key) != key1 {
		t.Errorf("Comm %+v has wrong key %s", comms2[0], *(comms2[0].Key))
	}

	comms3, err := createTaintAccessComms(n3, taintCollection)

	if len(comms3) != 2 {
		t.Errorf("Expected 2 commodities but got %d", len(comms3))
	}

	if *(comms3[0].Key) != key1 || *(comms3[1].Key) != key2 {
		if *(comms3[1].Key) == key1 && *(comms3[0].Key) == key2 {

		} else {
			t.Errorf("Wrong comms3 %+v", comms3)
		}
	}
}

func TestCreateTolerationAccessComms(t *testing.T) {
	taintCollection := getTaintCollection(nodes)

	testTolerationAccessComms(t, pod1, taintCollection, []string{key1, key2})

	testTolerationAccessComms(t, pod2, taintCollection, []string{key2})

	testTolerationAccessComms(t, pod3, taintCollection, []string{})
}

func newNodeWithTaints(id string, taints []api.Taint) *api.Node {
	node := &api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: id,
			UID:  types.UID(id),
		},

		Spec: api.NodeSpec{
			Taints: taints,
		},
	}

	return node
}

func newTaint(key, value string, effect api.TaintEffect) api.Taint {
	taint := api.Taint{
		Key:    key,
		Value:  value,
		Effect: effect,
	}

	return taint
}

func newPodWithTolerations(id, nodeName string, tolerations []api.Toleration) *api.Pod {
	return &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: id,
			UID:  types.UID(id),
		},

		Spec: api.PodSpec{
			NodeName:    nodeName,
			Tolerations: tolerations,
		},
	}
}

func newToleration(key, value string, effect api.TaintEffect, tolerationOp api.TolerationOperator) api.Toleration {
	toleration := api.Toleration{
		Key:      key,
		Value:    value,
		Effect:   effect,
		Operator: tolerationOp,
	}

	return toleration
}

func testTolerationAccessComms(t *testing.T, pod *api.Pod, taintCollection map[api.Taint]string, keys []string) {
	comms, err := createTolerationAccessComms(pod, taintCollection)

	if err != nil {
		t.Errorf("Error: %v", err)
	}

	if len(comms) != len(keys) {
		t.Errorf("Expected to get %d commodities but got %d", len(keys), len(comms))
	}

	// Don't care the order
	commsMap := make(map[string]struct{})
	for i := range comms {
		commsMap[comms[i].GetKey()] = struct{}{}
	}

	for _, key := range keys {
		if _, ok := commsMap[key]; !ok {
			t.Errorf("The commodity with key %s not found", key)
		}
	}
}

func newEntityDTO(id string, entityType proto.EntityDTO_EntityType, commBought []*proto.EntityDTO_CommodityBought) *proto.EntityDTO {
	return &proto.EntityDTO{
		Id:                &id,
		DisplayName:       &id,
		EntityType:        &entityType,
		CommoditiesBought: commBought,
	}
}

func createCommBoughtForPod(providerId string) []*proto.EntityDTO_CommodityBought {
	return []*proto.EntityDTO_CommodityBought{
		{
			ProviderId: &providerId,
			Bought:     []*proto.CommodityDTO{{}},
		},
	}
}
