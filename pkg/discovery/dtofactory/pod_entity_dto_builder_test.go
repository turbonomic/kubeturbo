package dtofactory

import (
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/client-go/pkg/api/v1"
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
	if _, err := builder.getPodCommoditiesBoughtFromQuota("quota1", &api.Pod{}, 100.0); err == nil {
		t.Errorf("Error thrown expected")
	}
}

func testGetCommoditiesWithError(t *testing.T, f func(pod *api.Pod, cpuFrequency float64) ([]*proto.CommodityDTO, error)) {
	if _, err := f(&api.Pod{}, 100.0); err == nil {
		t.Errorf("Error thrown expected")
	}
}
