package worker

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/client-go/pkg/api/v1"
	"testing"
)

func getEndPointSubset() api.EndpointSubset {

	targetRef1 := &api.ObjectReference{
		Kind:      "Pod",
		Name:      "pod-1",
		Namespace: "default",
	}
	targetRef2 := &api.ObjectReference{
		Kind:      "Node",
		Name:      "node-1",
		Namespace: "",
	}

	addr1 := api.EndpointAddress{
		TargetRef: targetRef1,
	}
	addr2 := api.EndpointAddress{
		TargetRef: targetRef2,
	}

	addrs := []api.EndpointAddress{addr1, addr2}

	result := api.EndpointSubset{
		Addresses: addrs,
	}

	return result
}

func TestFindPodEndpoints(t *testing.T) {
	svc := &api.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-1",
			Namespace: "default",
			UID:       "svc-1-uuid",
		},
	}

	subset := getEndPointSubset()

	endpoint := &api.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-1",
			Namespace: "default",
			UID:       "endpoint-1-uuid",
		},

		Subsets: []api.EndpointSubset{subset},
	}

	endpointMap := make(map[string]*api.Endpoints)
	svcId := util.GetEndpointsClusterID(endpoint)
	endpointMap[svcId] = endpoint

	result := findPodEndpoints(svc, endpointMap)
	if len(result) != 1 {
		t.Errorf("Failed to find service's pod endpoints: %d Vs. %d", 1, len(result))
	}
}
