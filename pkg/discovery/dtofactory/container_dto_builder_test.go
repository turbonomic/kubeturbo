package dtofactory

import (
	"fmt"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/client-go/pkg/api/v1"
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
	 * (controllable should be false, monitored should be true)
	 *
	 * Pod 2: controllable = true, monitored = true, parentKind = ReplicaSet
	 * (controllable should be true, monitored should be true)
	 *
	 * Pod 3: controllable = true, monitored = false, parentKind = ReplicaSet
	 * This pod has the "kubeturbo.io/monitored" attribute set to false.
	 * (controllable should be true, monitored should be false)
	 */
	expectedResult := []struct {
		Controllable bool
		Monitored    bool
	}{
		{true, true},
		{false, true},
		{true, true},
		{true, false},
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
		monitored := podutil.Monitored(pod)
		if controllable != expectedResult[i].Controllable {
			t.Errorf("Pod %d Controllable: expected %v, got %v", i,
				expectedResult[i].Controllable, controllable)
		}
		if monitored != expectedResult[i].Monitored {
			t.Errorf("Pod %d Monitored: expected %v, got %v", i,
				expectedResult[i].Monitored, monitored)
		}
	}
}

func dumpPodFlags(pods []*api.Pod) {
	// This code dumps the attributes of the saved topology
	for i, pod := range pods {
		parentKind, _, _ := podutil.GetPodParentInfo(pod)
		fmt.Printf("Pod %d: controllable = %v, monitored = %v, parentKind = %s\n", i,
			podutil.Controllable(pod),
			podutil.Monitored(pod),
			parentKind)
	}
}
