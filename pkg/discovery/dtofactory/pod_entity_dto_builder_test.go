package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	if _, err := builder.getQuotaCommoditiesBought("quota1", &api.Pod{}); err == nil {
		t.Errorf("Error thrown expected")
	}
}

func Test_podEntityDTOBuilder_createContainerPodData(t *testing.T) {
	podIP := "1.1.1.1"
	hostIP := "2.2.2.2"
	namespace := "foo"
	podName := "bar"
	port := "not-set"
	defCPUFreq := 1.0
	setCPUFreq := 2.0

	tests := []struct {
		name string
		pod  *api.Pod
		want *proto.EntityDTO_ContainerPodData
	}{
		{
			name: "test-pod-with-empty-IP-and-no-cpu-freq-found",
			pod:  createPodWithIPs("", hostIP),
			want: &proto.EntityDTO_ContainerPodData{
				HostingNodeCpuFrequency: &defCPUFreq,
			},
		},
		{
			name: "test-pod-with-same-host-IP-and-no-cpu-freq-found",
			pod:  createPodWithIPs(podIP, podIP),
			want: &proto.EntityDTO_ContainerPodData{
				HostingNodeCpuFrequency: &defCPUFreq,
			},
		},
		{
			name: "test-pod-with-different-IP-and-no-cpu-freq-found",
			pod:  createPodWithIPs(podIP, hostIP),
			want: &proto.EntityDTO_ContainerPodData{
				IpAddress:               &podIP,
				FullName:                &podName,
				Namespace:               &namespace,
				Port:                    &port,
				HostingNodeCpuFrequency: &defCPUFreq,
			},
		},
		{
			name: "test-pod-with-different-IP-and-cpu-freq-found",
			pod:  createPodWithIPs(podIP, hostIP),
			want: &proto.EntityDTO_ContainerPodData{
				IpAddress:               &podIP,
				FullName:                &podName,
				Namespace:               &namespace,
				Port:                    &port,
				HostingNodeCpuFrequency: &setCPUFreq,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := tt.pod
			nodeName := "test-node"
			pod.Spec.NodeName = nodeName
			if tt.name == "test-pod-with-different-IP-and-cpu-freq-found" {
				cpuFrequencyMetric := metrics.NewEntityStateMetric(metrics.NodeType, nodeName, metrics.CpuFrequency, setCPUFreq)
				builder.metricsSink.AddNewMetricEntries(cpuFrequencyMetric)
			}
			if got := builder.createContainerPodData(tt.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got = %v while want = %v", got, tt.want)
			}

		})
	}
}

func testGetCommoditiesWithError(t *testing.T,
	f func(pod *api.Pod, resType []metrics.ResourceType) ([]*proto.CommodityDTO, error)) {
	if _, err := f(createPodWithReadyCondition(), runningPodResCommTypeSold); err == nil {
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

func createPodWithReadyCondition() *api.Pod {
	return &api.Pod{
		Status: api.PodStatus{
			Conditions: []api.PodCondition{{
				Type:   api.PodReady,
				Status: api.ConditionTrue,
			}},
		},
	}
}

func TestPodEntityDTOBuilder(t *testing.T) {
	const toleration1Key = "key1"
	const toleration1Op = api.TolerationOpExists
	const toleration1Effect = api.TaintEffectNoSchedule
	const toleration2Key = "foo"
	const toleration2Op = api.TolerationOpEqual
	const toleration2Value = "bar"
	const toleration2Effect = api.TaintEffectNoExecute

	testPod1 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
			Tolerations: []api.Toleration{
				{
					Key:      toleration1Key,
					Operator: toleration1Op,
					Effect:   toleration1Effect,
				},
			},
		},
	}
	testPod2 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod2",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
			Tolerations: []api.Toleration{
				{
					Key:      toleration2Key,
					Operator: toleration2Op,
					Value:    toleration2Value,
					Effect:   toleration2Effect,
				},
			},
		},
	}
	testPod3 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod3",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
		},
	}

	tests := []struct {
		pod                      *api.Pod
		expectedMetricsAvailable bool
		propertyMatches          int
		propertyKey              string
		propertyValue            string
	}{
		{
			pod:                      testPod1,
			expectedMetricsAvailable: true,
			propertyMatches:          1,
			propertyKey:              toleration1Key,
			propertyValue:            string(toleration1Op) + " " + string(toleration1Effect) + " " + property.TolerationPropertyValueSuffix,
		},
		{
			pod:                      testPod2,
			expectedMetricsAvailable: false,
			propertyMatches:          0,
			propertyKey:              toleration2Key,
			propertyValue:            string(toleration2Op) + " " + toleration2Value + " " + string(toleration2Effect) + " " + property.TolerationPropertyValueSuffix,
		},
		{
			pod:                      testPod3,
			expectedMetricsAvailable: true,
			propertyMatches:          0,
		},
	}
	sink := metrics.NewEntityMetricSink()
	containerMetricsAvailableMetric := metrics.NewEntityStateMetric(metrics.PodType,
		util.PodKeyFunc(testPod1), metrics.MetricsAvailability, true)
	containerMetricsNotAvailableMetric := metrics.NewEntityStateMetric(metrics.PodType,
		util.PodKeyFunc(testPod2), metrics.MetricsAvailability, false)
	sink.AddNewMetricEntries(containerMetricsAvailableMetric, containerMetricsNotAvailableMetric)
	// pod3 does not have any isAvailability metrics created, we consider it as powerON

	podDTOBuilder := NewPodEntityDTOBuilder(sink, nil)
	for _, test := range tests {
		assert.Equal(t, podDTOBuilder.isContainerMetricsAvailable(test.pod), test.expectedMetricsAvailable)

		podDto, _ := podDTOBuilder.WithRunningPods([]*api.Pod{test.pod}).BuildEntityDTOs()
		for _, p := range podDto[0].GetEntityProperties() {
			matches := 0
			if p.GetNamespace() == property.VCTagsPropertyNamespace {
				if p.GetName() == test.propertyKey {
					assert.Equal(t, test.propertyValue, p.GetValue())
					matches++
				}
			}
			assert.Equal(t, test.propertyMatches, matches)
		}
	}
}
