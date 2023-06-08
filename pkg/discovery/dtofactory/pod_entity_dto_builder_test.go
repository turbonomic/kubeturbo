package dtofactory

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apiserver/pkg/util/feature"
)

var builder = &podEntityDTOBuilder{
	generalBuilder: newGeneralBuilder(metrics.NewEntityMetricSink()),
}

func Test_podEntityDTOBuilder_getPodCommoditiesSold_Error(t *testing.T) {
	testGetCommoditiesSoldWithError(t, builder.getPodCommoditiesSold)
}

func Test_podEntityDTOBuilder_getPodCommoditiesBought_Error(t *testing.T) {
	testGetCommoditiesBoughtWithError(t, builder.getPodCommoditiesBought)
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

func testGetCommoditiesSoldWithError(t *testing.T,
	f func(pod *api.Pod, resType []metrics.ResourceType) ([]*proto.CommodityDTO, error)) {
	if _, err := f(createPodWithReadyCondition(), runningPodResCommTypeSold); err == nil {
		t.Errorf("Error thrown expected")
	}
}

func testGetCommoditiesBoughtWithError(t *testing.T,
	f func(pod *api.Pod, isPodWithAffinity bool, resType []metrics.ResourceType) ([]*proto.CommodityDTO, error)) {
	if _, err := f(createPodWithReadyCondition(), false, runningPodResCommTypeSold); err == nil {
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

func TestContainerMetricsAvailability(t *testing.T) {
	testPod1 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
		},
	}
	testPod2 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod2",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
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
	}{
		{
			pod:                      testPod1,
			expectedMetricsAvailable: true,
		},
		{
			pod:                      testPod2,
			expectedMetricsAvailable: false,
		},
		{
			pod:                      testPod3,
			expectedMetricsAvailable: true,
		},
	}
	sink := metrics.NewEntityMetricSink()
	containerMetricsAvailableMetric := metrics.NewEntityStateMetric(metrics.PodType,
		util.PodKeyFunc(testPod1), metrics.MetricsAvailability, true)
	containerMetricsNotAvailableMetric := metrics.NewEntityStateMetric(metrics.PodType,
		util.PodKeyFunc(testPod2), metrics.MetricsAvailability, false)
	sink.AddNewMetricEntries(containerMetricsAvailableMetric, containerMetricsNotAvailableMetric)
	// pod3 does not have any isAvailability metrics created, we consider it as powerON

	podDTOBuilder := NewPodEntityDTOBuilder(sink, nil, nil)
	for _, test := range tests {
		assert.Equal(t, podDTOBuilder.isContainerMetricsAvailable(test.pod), test.expectedMetricsAvailable)
	}
}

func TestGetRegionZoneLabelCommodity(t *testing.T) {
	testPod1 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
		},
		Spec: api.PodSpec{
			NodeName: nodeName,
		},
	}

	nodeLabel1 := map[string]string{
		zoneLabelName:   "zone_value1",
		regionLabelName: "region_value1",
	}

	testNode1 := &api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			UID:    types.UID(nodeName),
			Labels: nodeLabel1,
		},
	}

	pvName1 := "pv1"
	testPv1 := &api.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName1,
			UID:  types.UID(pvName1),
		},
	}

	pod2PvMap := map[string][]repository.MountedVolume{
		util.GetPodClusterID(testPod1): {
			{
				MountName:  "mount-point-name",
				UsedVolume: testPv1,
			},
		},
	}
	nodeNameToNodeMap := map[string]*repository.KubeNode{
		nodeName: {
			KubeEntity: nil,
			Node:       testNode1,
		},
	}

	sink := metrics.NewEntityMetricSink()
	podDTOBuilder := NewPodEntityDTOBuilder(sink, nil, nil).WithPodToVolumesMap(pod2PvMap).WithNodeNameToNodeMap(nodeNameToNodeMap)
	var commoditiesBought []*proto.CommodityDTO
	var err error
	commoditiesBought, err = podDTOBuilder.getRegionZoneLabelCommodity(testPod1, commoditiesBought)
	if err != nil {
		t.Errorf("Failed to get region/zone label commodity")
	}
	var foundZoneComm, foundRegionComm bool
	for _, comm := range commoditiesBought {
		if comm.GetCommodityType() == proto.CommodityDTO_LABEL {
			if strings.Contains(comm.GetKey(), zoneLabelName) {
				foundZoneComm = true
			}
			if strings.Contains(comm.GetKey(), regionLabelName) {
				foundRegionComm = true
			}
		}
	}

	if !foundRegionComm || !foundZoneComm {
		t.Errorf("Can't find region/zone commodity")
	}
}

var (
	mockPodCommoditiesBoughtTypes = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
		metrics.CPURequest,
		metrics.MemoryRequest,
		metrics.NumPods,
		metrics.VStorage,
	}
)

func Test_getPodCommoditiesBought_NoAffinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
}

func Test_getPodCommoditiesBought_NodeAffinityWithNodeSelector(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			Affinity: &api.Affinity{
				NodeAffinity: &api.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
						NodeSelectorTerms: []api.NodeSelectorTerm{
							{
								MatchExpressions: []api.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: api.NodeSelectorOpIn,
										Values:   []string{"bar", "value2"},
									},
								},
							},
						},
					},
				},
			},
			NodeSelector: map[string]string{"test": "foo"},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 2, len(commoditiesBought))

	for _, commodity := range commoditiesBought {
		assert.Equal(t, proto.CommodityDTO_LABEL, *commodity.CommodityType)
	}
}

func Test_getPodCommoditiesBought_NodeAffinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			Affinity: &api.Affinity{
				NodeAffinity: &api.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
						NodeSelectorTerms: []api.NodeSelectorTerm{
							{
								MatchExpressions: []api.NodeSelectorRequirement{
									{
										Key:      "foo",
										Operator: api.NodeSelectorOpIn,
										Values:   []string{"bar", "value2"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
}

func Test_getPodCommoditiesBought_NodeSelector(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			NodeSelector: map[string]string{"test": "foo"},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 2, len(commoditiesBought))

	for _, commodity := range commoditiesBought {
		assert.Equal(t, proto.CommodityDTO_LABEL, *commodity.CommodityType)
	}
}

func Test_getPodCommoditiesBought_PodAffinityToSpread(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			Affinity: &api.Affinity{
				NodeAffinity: &api.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &api.NodeSelector{
						NodeSelectorTerms: []api.NodeSelectorTerm{
							{
								MatchExpressions: []api.NodeSelectorRequirement{
									{
										Key:      "kubernetes.io/arch",
										Operator: api.NodeSelectorOpIn,
										Values:   []string{"amd64"},
									},
								},
							},
						},
					},
				},
				PodAffinity: &api.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"store"},
									},
								},
							},
							TopologyKey: "kubernetes.io/arch",
						},
					},
				},
			},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
}

func Test_getPodCommoditiesBought_SpreadWithPodAntiAffinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			Affinity: &api.Affinity{
				PodAntiAffinity: &api.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"store"},
									},
								},
							},
							TopologyKey: "kubernetes.io/arch",
						},
					},
				},
			},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
}

func Test_getPodCommoditiesBought_PodAntiAffinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	mockPod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			UID:       "test-pod-1-UID",
		},
		Spec: api.PodSpec{
			Affinity: &api.Affinity{
				PodAntiAffinity: &api.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"store"},
									},
								},
							},
							Namespaces:  []string{"default"},
							TopologyKey: "kubernetes.io/arch",
						},
					},
				},
			},
		},
	}

	commoditiesBought, err := builder.getPodCommoditiesBought(mockPod, true, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
}
