package dtofactory

import (
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"reflect"
	"testing"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

func Test_getCommoditiesSold(t *testing.T) {
	podIP := "1.1.1.1"
	pod1 := createPodWithIPs(podIP, "2.2.2.2")
	type args struct {
		pod   *api.Pod
		index int
	}
	tests := []struct {
		name    string
		args    args
		want    []*proto.CommodityDTO
		wantErr bool
	}{
		{
			name: "test-container-with-index-0",
			args: args{
				pod:   pod1,
				index: 0,
			},
			want: []*proto.CommodityDTO{
				createCommodity(proto.CommodityDTO_APPLICATION, podIP),
			},
		},
		{
			name: "test-container-with-index-other-than-0",
			args: args{
				pod:   pod1,
				index: 1,
			},
			want: []*proto.CommodityDTO{
				createCommodity(proto.CommodityDTO_APPLICATION, podIP+"-1"),
			},
		},
	}

	var sink *metrics.EntityMetricSink
	var podClusterIDToServiceMap map[string]*api.Service
	podClusterIDToServiceMap = make(map[string]*api.Service)
	podId := "default/pod1"
	podClusterIDToServiceMap[podId] = &api.Service{}
	clusterScraper := &cluster.ClusterScraper{}

	applicationEntityDTOBuilder := NewApplicationEntityDTOBuilder(sink, mockClusterSummary(podClusterIDToServiceMap), clusterScraper)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applicationEntityDTOBuilder.getCommoditiesSold(tt.args.pod, tt.args.index)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCommoditiesSold() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getCommoditiesSold() = %v, want %v", got, tt.want)
			}
		})
	}
}

func createCommodity(commType proto.CommodityDTO_CommodityType, key string) *proto.CommodityDTO {
	capacity := sdkbuilder.AppCommodityDefaultCapacity
	return &proto.CommodityDTO{
		CommodityType: &commType,
		Key:           &key,
		Capacity:      &capacity,
	}
}

func mockClusterSummary(podClusterIDToServiceMap map[string]*api.Service) *repository.ClusterSummary {
	var nodes []*api.Node
	cluster := repository.NewKubeCluster(testClusterId, nodes)
	clusterSummary := repository.CreateClusterSummary(cluster)
	clusterSummary.PodClusterIDToServiceMap = podClusterIDToServiceMap
	return clusterSummary
}
