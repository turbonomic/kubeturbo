package dtofactory

import (
	"testing"

	"reflect"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var builder = &podEntityDTOBuilder{
	generalBuilder: newGeneralBuilder(metrics.NewEntityMetricSink()),
}

func Test_podEntityDTOBuilder_getPodCommoditiesSold_Error(t *testing.T) {
	testGetCommoditiesWithError(t, builder.getPodCommoditiesSold)
}

func Test_podEntityDTOBuilder_getPodCommoditiesBought_Error(t *testing.T) {
	testGetCommoditiesWithError(t, builder.getPodCommoditiesBought)
}

func Test_podEntityDTOBuilder_getPodCommoditiesBoughtFromQuota_Error(t *testing.T) {
	if _, err := builder.getQuotaCommoditiesBought("quota1", &api.Pod{}, 100.0); err == nil {
		t.Errorf("Error thrown expected")
	}
}

func Test_podEntityDTOBuilder_createContainerPodData(t *testing.T) {
	podIP := "1.1.1.1"
	hostIP := "2.2.2.2"
	namespace := "foo"
	podName := "bar"
	port := "not-set"

	tests := []struct {
		name string
		pod  *api.Pod
		want *proto.EntityDTO_ContainerPodData
	}{
		{
			name: "test-pod-with-empty-IP",
			pod:  createPodWithIPs("", hostIP),
			want: nil,
		},
		{
			name: "test-pod-with-same-host-IP",
			pod:  createPodWithIPs(podIP, podIP),
			want: nil,
		},
		{
			name: "test-pod-with-different-IP",
			pod:  createPodWithIPs(podIP, hostIP),
			want: &proto.EntityDTO_ContainerPodData{
				IpAddress: &podIP,
				FullName:  &podName,
				Namespace: &namespace,
				Port:      &port,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := builder.createContainerPodData(tt.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got = %v while want = %v", got, tt.want)
			}

		})
	}
}

func testGetCommoditiesWithError(t *testing.T, f func(pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error)) {
	if _, err := f(&api.Pod{}, 100.0); err == nil {
		t.Errorf("Error thrown expected")
	}
}

func createPodWithIPs(podIP, hostIP string) *api.Pod {
	status := api.PodStatus{
		PodIP:  podIP,
		HostIP: hostIP,
	}

	return &api.Pod{
		Status:     status,
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
	}
}
