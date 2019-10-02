package dtofactory

import (
	"fmt"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
	"testing"
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
		controllable := podutil.Controllable(pod)
		if controllable != expectedResult[i].Controllable {
			t.Errorf("Pod %d Controllable: expected %v, got %v", i,
				expectedResult[i].Controllable, controllable)
		}
	}
}

func dumpPodFlags(pods []*api.Pod) {
	// This code dumps the attributes of the saved topology
	for i, pod := range pods {
		parentKind, _, _ := podutil.GetPodParentInfo(pod)
		fmt.Printf("Pod %d: controllable = %v, parentKind = %s\n", i,
			podutil.Controllable(pod),
			parentKind)
	}
}
