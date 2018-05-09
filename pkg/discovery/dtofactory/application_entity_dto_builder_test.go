package dtofactory

import (
	"reflect"
	"testing"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/client-go/pkg/api/v1"
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
				createCommodity(proto.CommodityDTO_TRANSACTION, podIP, defaultTransactionCapacity),
				createCommodity(proto.CommodityDTO_RESPONSE_TIME, podIP, defaultRespTimeCapacity),
			},
		},
		{
			name: "test-container-with-index-other-than-0",
			args: args{
				pod:   pod1,
				index: 1,
			},
			want: []*proto.CommodityDTO{
				createCommodity(proto.CommodityDTO_TRANSACTION, podIP+"-1", defaultTransactionCapacity),
				createCommodity(proto.CommodityDTO_RESPONSE_TIME, podIP+"-1", defaultRespTimeCapacity),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getCommoditiesSold(tt.args.pod, tt.args.index)
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

func createCommodity(commType proto.CommodityDTO_CommodityType, key string, capacity float64) *proto.CommodityDTO {
	return &proto.CommodityDTO{
		CommodityType: &commType,
		Key:           &key,
		Capacity:      &capacity,
	}
}
