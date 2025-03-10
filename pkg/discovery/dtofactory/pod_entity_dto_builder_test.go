package dtofactory

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/orm/api/v1alpha1"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apiserver/pkg/util/feature"
)

var builder = &podEntityDTOBuilder{
	generalBuilder: newGeneralBuilder(metrics.NewEntityMetricSink(), nil),
}

func Test_podEntityDTOBuilder_getPodCommoditiesSold_Error(t *testing.T) {
	testGetCommoditiesSoldWithError(t, builder.getPodCommoditiesSold)
}

func Test_podEntityDTOBuilder_getPodCommoditiesBought_Error(t *testing.T) {
	testGetCommoditiesBoughtWithError(t, builder.getPodCommoditiesBoughtFromNode)
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

	podDTOBuilder := NewPodEntityDTOBuilder(sink, nil, nil, nil)
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
	podDTOBuilder := NewPodEntityDTOBuilder(sink, nil, nil, nil).WithPodToVolumesMap(pod2PvMap).WithNodeNameToNodeMap(nodeNameToNodeMap)
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
	builder.affinityMapper = podaffinity.NewAffinityMapper(nil, nil)
	commoditiesBought, err := builder.getPodCommoditiesBoughtFromNode(mockPod, mockPodCommoditiesBoughtTypes)

	// No LABEL commodities for pod with no affinity rule
	assert.Nil(t, err)
	assert.Equal(t, 0, len(commoditiesBought))
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
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

	// The affinities are preprocessed and presented as bunch of sets to pod and node dto builders
	// NodeSelectors are processed in the dto builders
	// The Affinity field in the test spec above is unused, but left here for representation
	// The affinity processing and the resulting maps are tested in affinity processing unit test
	// podsWithAffinities here carries the processed result
	builder.podsWithLabelBasedAffinities = sets.NewString(mockPod.Namespace + "/" + mockPod.Name)
	commoditiesBought, err := builder.getPodCommoditiesBoughtFromNode(mockPod, mockPodCommoditiesBoughtTypes)
	// Cleanup
	builder.podsWithLabelBasedAffinities = nil
	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 2, len(commoditiesBought))

	for _, commodity := range commoditiesBought {
		assert.Equal(t, proto.CommodityDTO_LABEL, *commodity.CommodityType)
	}
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
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

	// The affinities are preprocessed and presented as bunch of sets to pod and node dto builders
	// NodeSelectors are processed in the dto builders
	// The Affinity field in the test spec above is unused, but left here for representation
	// The affinity processing and the resulting maps are tested in affinity processing unit test
	// podsWithAffinities here carries the processed result
	builder.podsWithLabelBasedAffinities = sets.NewString(mockPod.Namespace + "/" + mockPod.Name)
	commoditiesBought, err := builder.getPodCommoditiesBoughtFromNode(mockPod, mockPodCommoditiesBoughtTypes)
	// Cleanup
	builder.podsWithLabelBasedAffinities = nil

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commoditiesBought[0].CommodityType)
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
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

	commoditiesBought, err := builder.getPodCommoditiesBoughtFromNode(mockPod, mockPodCommoditiesBoughtTypes)

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))

	for _, commodity := range commoditiesBought {
		assert.Equal(t, proto.CommodityDTO_LABEL, *commodity.CommodityType)
	}
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
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

	// The affinities are preprocessed and presented as bunch of sets to pod and node dto builders
	// NodeSelectors are processed in the dto builders
	// The Affinity field in the test spec above is unused, but left here for representation
	// The affinity processing and the resulting maps are tested in affinity processing unit test
	// hostnameSpreadPods and podsToControllers in this test here carries the processed result
	builder.hostnameSpreadPods = sets.NewString(mockPod.Namespace + "/" + mockPod.Name)
	builder.podsToControllers = map[string]string{mockPod.Namespace + "/" + mockPod.Name: "dummy-controller"}
	builder.affinityMapper = podaffinity.NewAffinityMapper(nil, nil)
	commoditiesBought, err := builder.getPodCommoditiesBoughtFromNode(mockPod, mockPodCommoditiesBoughtTypes)
	// Cleanup
	builder.hostnameSpreadPods = nil
	builder.podsToControllers = nil

	assert.Nil(t, err)
	assert.NotEmpty(t, commoditiesBought)
	assert.Equal(t, 1, len(commoditiesBought))
	assert.Equal(t, proto.CommodityDTO_SEGMENTATION, *commoditiesBought[0].CommodityType)
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
}

func Test_getPodCommoditiesBoughtFromNodeGroup_p2p_anti_affinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=true")

	table := []struct {
		topologyKey         string
		nodes               []*api.Node
		pods                []*api.Pod
		expectCommodityKey  string
		expectCommodityType proto.CommodityDTO_CommodityType
	}{
		{
			topologyKey: "zone",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"zone": "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone": "zone1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
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
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "zone",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "zone|testns/webserver|testns/db",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY,
		},
		{
			topologyKey: "kubernetes.io/hostname",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
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
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "kubernetes.io/hostname|testns/webserver|testns/db",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY,
		},
		{
			topologyKey: "kubernetes.io/hostname",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						UID:       types.UID("pod0"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "store"},
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
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						UID:       types.UID("pod1"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "store"},
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
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "kubernetes.io/hostname|testns/pod0|testns/pod1",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY,
		},
		{
			topologyKey: "kubernetes.io/zone",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"kubernetes.io/zone": "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"kubernetes.io/zone": "zone1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						UID:       types.UID("pod0"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "store"},
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
										TopologyKey: "kubernetes.io/zone",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						UID:       types.UID("pod1"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "store"},
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
										TopologyKey: "kubernetes.io/zone",
									},
								},
							},
						},
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "kubernetes.io/zone|testns/pod0|testns/pod1",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_ANTI_AFFINITY,
		},
	}

	oldClusterSummary := builder.clusterSummary
	for _, item := range table {
		// construct affinity map for generating p2p affinity commoditiesBought commodities
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		nodeInfoLister := podaffinity.NewNodeInfoLister(clusterSummary)
		builder.clusterSummary = clusterSummary
		am := podaffinity.NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoLister)
		srcPodInfo, _ := podaffinity.NewPodInfo(item.pods[0])
		dstPodInfo, _ := podaffinity.NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]
		am.BuildAffinityMaps(srcPodInfo.RequiredAntiAffinityTerms, srcPodInfo, dstPodInfo, dstNode, podaffinity.AntiAffinity)
		builder.WithAffinityMapper(am)
		var commoditiesBought []*proto.CommodityDTO
		var err error
		for i, pod := range item.pods {
			// check correctness of each generated commoditiesBought
			if item.topologyKey == "kubernetes.io/hostname" {
				commoditiesBought, err = builder.getPodCommoditiesBoughtFromNode(pod, mockPodCommoditiesBoughtTypes)
			} else {
				commoditiesBought, err = builder.getPodCommoditiesBoughtFromNodeGroup(pod)
			}

			assert.True(t, err == nil)
			for _, commodityBought := range commoditiesBought {
				assert.Equal(t, *commodityBought.Key, item.expectCommodityKey)
				assert.Equal(t, *commodityBought.CommodityType, item.expectCommodityType)
				if i == 0 {
					assert.Equal(t, *commodityBought.Used, float64(1))
				} else {
					assert.Equal(t, *commodityBought.Used, podaffinity.NONE)
				}
			}
		}
	}
	// revet global variable sets to cluster summary of the builder instance
	builder.clusterSummary = oldClusterSummary
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=false")
}

func Test_getAffinityRelatedSegmentationCommoditiesFromNodegroup(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("SegmentationBasedTopologySpread=true")

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
	podQualifiedName := mockPod.Namespace + "/" + mockPod.Name

	test_cases := []struct {
		otherSpreadPods                    sets.String
		podsToControllers                  map[string]string
		podNonHostnameAntiTermTopologyKeys map[string]sets.String
		expectedCommoditiesBoughtIds       sets.String
	}{
		{
			otherSpreadPods: sets.NewString(podQualifiedName),
			podsToControllers: map[string]string{
				podQualifiedName: "dummy-controller",
			},
			podNonHostnameAntiTermTopologyKeys: map[string]sets.String{
				podQualifiedName: sets.NewString("zone", "region"),
			},
			expectedCommoditiesBoughtIds: sets.NewString("zone@dummy-controller", "region@dummy-controller"),
		},
		{
			otherSpreadPods: nil,
			podsToControllers: map[string]string{
				podQualifiedName: "dummy-controller",
			},
			podNonHostnameAntiTermTopologyKeys: map[string]sets.String{
				podQualifiedName: sets.NewString("zone", "region"),
			},
			expectedCommoditiesBoughtIds: sets.String{},
		},
		{
			otherSpreadPods:   sets.NewString(podQualifiedName),
			podsToControllers: nil,
			podNonHostnameAntiTermTopologyKeys: map[string]sets.String{
				podQualifiedName: sets.NewString("zone", "region"),
			},
			expectedCommoditiesBoughtIds: sets.String{},
		},
		{
			otherSpreadPods: sets.NewString(podQualifiedName),
			podsToControllers: map[string]string{
				podQualifiedName: "dummy-controller",
			},
			podNonHostnameAntiTermTopologyKeys: nil,
			expectedCommoditiesBoughtIds:       sets.String{},
		},
	}

	for _, test_case := range test_cases {
		builder.WithOtherSpreadPods(test_case.otherSpreadPods)
		builder.WithPodsToControllers(test_case.podsToControllers)
		builder.WithPodNonHostnameAntiTermTopologyKeys(test_case.podNonHostnameAntiTermTopologyKeys)

		commoditiesBought, err := builder.getSegmentationCommoditiesBoughtFromNodegroup(podQualifiedName)
		assert.True(t, err == nil)

		actualCommoditiesBoughtIds := sets.String{}
		for _, commodityBought := range commoditiesBought {
			actualCommoditiesBoughtIds = actualCommoditiesBoughtIds.Insert(*commodityBought.Key)
		}
		assert.Equal(t, actualCommoditiesBoughtIds, test_case.expectedCommoditiesBoughtIds)
	}
	feature.DefaultMutableFeatureGate.Set("SegmentationBasedTopologySpread=false")
}

func Test_getAffinityLabelOrSegmentationCommoditiesFromNode(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("SegmentationBasedTopologySpread=true")

	mockPod1 := &api.Pod{
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
	mockPod2 := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-2",
			Namespace: "test-namespace",
			UID:       "test-pod-2-UID",
		},
	}
	podQualifiedName1 := mockPod1.Namespace + "/" + mockPod1.Name
	podQualifiedName2 := mockPod2.Namespace + "/" + mockPod2.Name

	test_cases := []struct {
		podInQuestion                  string
		podsWithAffinities             sets.String
		podsToControllers              map[string]string
		hostnameSpreadPods             sets.String
		expectedCommoditiesBoughtIds   sets.String
		expectedCommoditiesBoughtTypes sets.String
	}{
		{
			podInQuestion:      podQualifiedName1,
			podsWithAffinities: sets.NewString(podQualifiedName1),
			podsToControllers: map[string]string{
				podQualifiedName1: "dummy-controller-1",
				podQualifiedName2: "dummy-controller-2",
			},
			hostnameSpreadPods:             sets.NewString(podQualifiedName2),
			expectedCommoditiesBoughtIds:   sets.NewString("dummy-controller-1"),
			expectedCommoditiesBoughtTypes: sets.NewString("LABEL"),
		},
		{
			podInQuestion:      podQualifiedName2,
			podsWithAffinities: sets.NewString(podQualifiedName1),
			podsToControllers: map[string]string{
				podQualifiedName1: "dummy-controller-1",
				podQualifiedName2: "dummy-controller-2",
			},
			hostnameSpreadPods:             sets.NewString(podQualifiedName2),
			expectedCommoditiesBoughtIds:   sets.NewString("dummy-controller-2"),
			expectedCommoditiesBoughtTypes: sets.NewString("SEGMENTATION"),
		},
		{
			podInQuestion:                  podQualifiedName1,
			podsWithAffinities:             sets.NewString(podQualifiedName1),
			podsToControllers:              nil,
			hostnameSpreadPods:             nil,
			expectedCommoditiesBoughtIds:   sets.NewString(podQualifiedName1),
			expectedCommoditiesBoughtTypes: sets.NewString("LABEL"),
		},
		{
			podInQuestion:                  podQualifiedName2,
			podsWithAffinities:             nil,
			podsToControllers:              nil,
			hostnameSpreadPods:             sets.NewString(podQualifiedName2),
			expectedCommoditiesBoughtIds:   sets.String{},
			expectedCommoditiesBoughtTypes: sets.String{},
		},
	}

	for _, test_case := range test_cases {
		builder.WithPodsWithLabelBasedAffinities(test_case.podsWithAffinities)
		builder.WithPodsToControllers(test_case.podsToControllers)
		builder.WithHostnameSpreadPods(test_case.hostnameSpreadPods)

		commoditiesBought, err := builder.getAffinityLabelOrSegmentationCommoditiesFromNode(test_case.podInQuestion)
		assert.True(t, err == nil)

		actualCommoditiesBoughtIds := sets.String{}
		actualCommoditiesTypes := sets.String{}
		for _, commodityBought := range commoditiesBought {
			actualCommoditiesBoughtIds = actualCommoditiesBoughtIds.Insert(*commodityBought.Key)
			actualCommoditiesTypes = actualCommoditiesTypes.Insert(commodityBought.GetCommodityType().String())
		}
		assert.Equal(t, actualCommoditiesBoughtIds, test_case.expectedCommoditiesBoughtIds)
		assert.Equal(t, actualCommoditiesTypes, test_case.expectedCommoditiesBoughtTypes)
	}
	feature.DefaultMutableFeatureGate.Set("SegmentationBasedTopologySpread=false")
}

func Test_getPodCommoditiesBoughtFromNodeGroup_p2p_affinity(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=true")

	table := []struct {
		topologyKey         string
		nodes               []*api.Node
		pods                []*api.Pod
		expectCommodityKey  string
		expectCommodityType proto.CommodityDTO_CommodityType
	}{
		{
			topologyKey: "zone",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"zone": "zone0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone": "zone1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
					},
					Spec: api.PodSpec{
						Affinity: &api.Affinity{
							PodAffinity: &api.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "app",
													Operator: metav1.LabelSelectorOpIn,
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "zone",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "zone|testns/webserver|testns/db",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_AFFINITY,
		},
		{
			topologyKey: "kubernetes.io/hostname",
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node0",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node1",
						},
					},
				},
			},
			pods: []*api.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webserver",
						UID:       types.UID("webserver"),
						Namespace: "testns",
					},
					Spec: api.PodSpec{
						Affinity: &api.Affinity{
							PodAffinity: &api.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "app",
													Operator: metav1.LabelSelectorOpIn,
													Values:   []string{"db"},
												},
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						NodeName: "node0",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "db",
						UID:       types.UID("db"),
						Namespace: "testns",
						Labels:    map[string]string{"app": "db"},
					},
					Spec: api.PodSpec{
						NodeName: "node1",
					},
					Status: api.PodStatus{
						Phase: api.PodRunning,
					},
				},
			},
			expectCommodityKey:  "kubernetes.io/hostname|testns/webserver|testns/db",
			expectCommodityType: proto.CommodityDTO_PEER_TO_PEER_AFFINITY,
		},
	}

	oldClusterSummary := builder.clusterSummary
	for _, item := range table {
		// construct affinity map for generating p2p affinity commoditiesBought commodities
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		nodeInfoLister := podaffinity.NewNodeInfoLister(clusterSummary)
		builder.clusterSummary = clusterSummary
		am := podaffinity.NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoLister)
		srcPodInfo, _ := podaffinity.NewPodInfo(item.pods[0])
		dstPodInfo, _ := podaffinity.NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]
		am.BuildAffinityMaps(srcPodInfo.RequiredAffinityTerms, srcPodInfo, dstPodInfo, dstNode, podaffinity.Affinity)
		builder.WithAffinityMapper(am)

		for i, pod := range item.pods {
			// check correctness of each generated commoditiesBought
			commoditiesBought, err := builder.getPodCommoditiesBoughtFromNodeGroup(pod)
			assert.True(t, err == nil)
			for _, commodityBought := range commoditiesBought {
				assert.Equal(t, *commodityBought.Key, item.expectCommodityKey)
				assert.Equal(t, *commodityBought.CommodityType, item.expectCommodityType)
				if i == 0 {
					assert.Equal(t, *commodityBought.Used, podaffinity.NONE)
				} else {
					assert.Equal(t, *commodityBought.Used, float64(1))
				}
			}
		}
	}
	// revet global variable sets to cluster summary of the builder instance
	builder.clusterSummary = oldClusterSummary
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=false")
}

func Test_getController_(t *testing.T) {
	testCases := []struct {
		name            string
		ownerReferences []metav1.OwnerReference
		expectedExists  bool
	}{
		{
			name: "Controller Exists",
			ownerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "TEST",
				Name:       "TEST",
				UID:        types.UID("TEST"),
			}},
			expectedExists: true,
		},
		{
			name:            "Empty OwnerInfo",
			ownerReferences: []metav1.OwnerReference{},
			expectedExists:  false,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mockPod := &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: test.ownerReferences,
				},
			}

			mockClusterSummary := mockClusterSummaryWithClusterIdOnly(testClusterId)
			mockClusterSummary.ControllerMap = map[string]*repository.K8sController{}
			for _, or := range test.ownerReferences {
				mockClusterSummary.ControllerMap[string(or.UID)] = &repository.K8sController{}
			}

			builder := NewPodEntityDTOBuilder(mockMetricsSink(), mockClusterSummary, nil, nil)

			_, actualExists := builder.getController(mockPod)

			assert.Equal(t, test.expectedExists, actualExists)
		})
	}
}

type MockORMHandler struct{ mock.Mock }

func newMockORMHAndler() *MockORMHandler { return &MockORMHandler{} }

// temporary add this for compatibility, will remove it in another issue
func (ormHandler *MockORMHandler) CheckAndReplaceWithFirstClassOwners(obj *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {
	return map[*unstructured.Unstructured][]string{
		obj: ownedResourcePaths,
	}, nil
}

func (ormHandler *MockORMHandler) HasORM(controller *repository.K8sController) bool {
	args := ormHandler.Called(controller)
	return args.Bool(0)
}

func (ormHandler *MockORMHandler) GetOwnerResourcesForOwnedResources(
	ownedObj *unstructured.Unstructured,
	ownerReference util.OwnerInfo,
	ownedResourcePaths []string,
) (*resourcemapping.OwnerResources, error) {
	args := ormHandler.Called(ownedObj, ownerReference, ownedResourcePaths)
	ownerResources := args.Get(0).(resourcemapping.OwnerResources)
	return &ownerResources, args.Error(1)
}

func (ormHandler *MockORMHandler) DiscoverORMs() {}

func Test_hasSLOScaleORM(t *testing.T) {
	isController := true
	mockOwnerReferences := []metav1.OwnerReference{{
		APIVersion: "TEST",
		Kind:       "TEST",
		Name:       t.Name(),
		UID:        types.UID("TEST"),
		Controller: &isController,
	}}
	mockOwnerResources := resourcemapping.OwnerResources{
		OwnerResourcesMap: map[string][]v1alpha1.ResourcePath{"test": nil},
	}

	testCases := []struct {
		name                string
		ownerReferences     []metav1.OwnerReference
		hasOrm              bool
		ownerResources      resourcemapping.OwnerResources
		ownerResourcesError error
		expectedResult      bool
	}{
		{
			name:            "Has SLO Scale ORM",
			ownerReferences: mockOwnerReferences,
			hasOrm:          true,
			ownerResources:  mockOwnerResources,
			expectedResult:  true,
		},
		{
			name:            "No Owner",
			ownerReferences: []metav1.OwnerReference{},
			expectedResult:  false,
		}, {
			name:            "No ORM",
			ownerReferences: mockOwnerReferences,
			hasOrm:          false,
			expectedResult:  false,
		}, {
			name:            "No ORM SLO Mapping",
			ownerReferences: mockOwnerReferences,
			hasOrm:          true,
			expectedResult:  false,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mockController := &repository.K8sController{
				OwnerReferences: test.ownerReferences,
			}

			mockClusterSummary := mockClusterSummaryWithClusterIdOnly(testClusterId)
			mockClusterSummary.ControllerMap = map[string]*repository.K8sController{}
			for _, or := range test.ownerReferences {
				mockClusterSummary.ControllerMap[string(or.UID)] = &repository.K8sController{}
			}

			mockOrmHandler := newMockORMHAndler()
			mockOrmHandler.On("HasORM", mockController).Return(test.hasOrm)
			mockOrmHandler.
				On("GetOwnerResourcesForOwnedResources", mock.Anything, mock.Anything, mock.Anything).
				Return(test.ownerResources, test.ownerResourcesError)

			builder := NewPodEntityDTOBuilder(mockMetricsSink(), mockClusterSummary, nil, nil)
			builder.WithORMHandler(mockOrmHandler)

			actualResult := builder.hasSLOScaleORM(mockController)

			assert.Equal(t, test.expectedResult, actualResult)
		})
	}
}

func generateMockOwnerReference(kind string) []metav1.OwnerReference {
	isController := true
	return []metav1.OwnerReference{{
		APIVersion: "TEST",
		Kind:       kind,
		Name:       "TEST",
		UID:        types.UID("TEST"),
		Controller: &isController,
	}}
}

func Test_isHorizontallyScalable(t *testing.T) {
	mockOwnerResources := resourcemapping.OwnerResources{
		OwnerResourcesMap: map[string][]v1alpha1.ResourcePath{"test": nil},
	}

	testCases := []struct {
		name                string
		ownerReferences     []metav1.OwnerReference
		controllerKind      string
		hasParent           bool
		labels              map[string]string
		hasORM              bool
		ownerResources      resourcemapping.OwnerResources
		ownerResourcesError error
		expectedResult      bool
	}{
		{
			name:            "Horizontall Scalable",
			ownerReferences: generateMockOwnerReference(commonutil.KindDeployment),
			controllerKind:  commonutil.KindDeployment,
			hasParent:       false,
			hasORM:          true,
			ownerResources:  mockOwnerResources,
			expectedResult:  true,
		}, {
			name:            "Horizontall Scalable ORM",
			ownerReferences: generateMockOwnerReference(commonutil.KindDeployment),
			controllerKind:  commonutil.KindDeployment,
			hasParent:       true,
			hasORM:          true,
			ownerResources:  mockOwnerResources,
			expectedResult:  true,
		}, {
			name:            "No Controller",
			ownerReferences: []metav1.OwnerReference{},
			expectedResult:  false,
		}, {
			name:            "ORM - Has SkipOperatorLabel",
			ownerReferences: generateMockOwnerReference("TEST"),
			controllerKind:  "TEST",
			hasParent:       true,
			labels:          map[string]string{commonutil.SkipOperatorLabel: "true"},
			expectedResult:  true,
		}, {
			name:            "ORM - Unsupported Controller Type",
			ownerReferences: generateMockOwnerReference("TEST"),
			controllerKind:  "TEST",
			hasParent:       true,
			expectedResult:  false,
		}, {
			name:            "ORM - No Replica Mapping",
			ownerReferences: generateMockOwnerReference(commonutil.KindDeployment),
			controllerKind:  commonutil.KindDeployment,
			hasParent:       true,
			hasORM:          true,
			ownerResources:  resourcemapping.OwnerResources{},
			expectedResult:  false,
		}, {
			name:            "Unscalable Controller Kind",
			ownerReferences: generateMockOwnerReference(commonutil.KindDeployment),
			controllerKind:  "TEST",
			hasParent:       false,
			hasORM:          true,
			ownerResources:  resourcemapping.OwnerResources{},
			expectedResult:  false,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mockPod := &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "TEST",
					Name:            test.name,
					OwnerReferences: test.ownerReferences},
			}

			mockController := &repository.K8sController{
				Kind:            test.controllerKind,
				OwnerReferences: test.ownerReferences,
				HasParent:       test.hasParent,
				Labels:          test.labels,
			}

			kubeCluster := repository.NewKubeCluster("TEST", []*api.Node{})
			clusterSummary := repository.CreateClusterSummary(kubeCluster)
			clusterSummary.ControllerMap = map[string]*repository.K8sController{}
			if len(test.ownerReferences) > 0 {
				clusterSummary.ControllerMap[string(test.ownerReferences[0].UID)] = mockController
			}
			builder.clusterSummary = clusterSummary

			mockOrmHandler := newMockORMHAndler()
			mockOrmHandler.On("HasORM", mock.Anything).Return(test.hasORM)
			mockOrmHandler.
				On("GetOwnerResourcesForOwnedResources", mock.Anything, mock.Anything, mock.Anything).
				Return(test.ownerResources, test.ownerResourcesError)
			builder.WithORMHandler(mockOrmHandler)

			actualResult := builder.isHorizontallyScalable(mockPod)

			assert.Equal(t, test.expectedResult, actualResult)
		})
	}
}

func Test_isMovable(t *testing.T) {
	testCases := []struct {
		name                             string
		controllerKind                   string
		ownerReferences                  []metav1.OwnerReference
		ownerReferenceError              error
		controllerDoesNotExist           bool
		featureFlagVirtualMachinePodMove bool
		expectedResult                   bool
	}{
		{
			name:            "movable kind",
			controllerKind:  commonutil.KindReplicaSet,
			ownerReferences: generateMockOwnerReference(commonutil.KindReplicaSet),
			expectedResult:  true,
		}, {
			name:            "unmovable kind",
			controllerKind:  commonutil.KindJob,
			ownerReferences: generateMockOwnerReference(commonutil.KindJob),
			expectedResult:  false,
		}, {
			name:            "bare pod",
			ownerReferences: []metav1.OwnerReference{},
			expectedResult:  true,
		}, {
			name:                   "controller does not exist",
			ownerReferences:        generateMockOwnerReference(commonutil.KindReplicaSet),
			controllerDoesNotExist: true,
			expectedResult:         false,
		}, {
			name:                             "VirtualMachinePodMove flag enabled",
			controllerKind:                   commonutil.KindVirtualMachineInstance,
			ownerReferences:                  generateMockOwnerReference(commonutil.KindVirtualMachineInstance),
			featureFlagVirtualMachinePodMove: true,
			expectedResult:                   true,
		}, {
			name:                             "VirtualMachinePodMove flag disabled",
			controllerKind:                   commonutil.KindVirtualMachineInstance,
			ownerReferences:                  generateMockOwnerReference(commonutil.KindVirtualMachineInstance),
			featureFlagVirtualMachinePodMove: false,
			expectedResult:                   true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mockPod := &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "TEST",
					Name:            test.name,
					OwnerReferences: test.ownerReferences},
			}

			feature.DefaultMutableFeatureGate.Set(
				fmt.Sprintf("VirtualMachinePodMove=%t", test.featureFlagVirtualMachinePodMove))

			kubeCluster := repository.NewKubeCluster("TEST", []*api.Node{})
			clusterSummary := repository.CreateClusterSummary(kubeCluster)
			clusterSummary.ControllerMap = map[string]*repository.K8sController{}
			if len(test.ownerReferences) > 0 && !test.controllerDoesNotExist {
				mockController := &repository.K8sController{
					Kind:            test.controllerKind,
					OwnerReferences: test.ownerReferences,
				}
				clusterSummary.ControllerMap[string(test.ownerReferences[0].UID)] = mockController
			}
			builder.clusterSummary = clusterSummary

			actualResult := builder.isMovable(mockPod)

			assert.Equal(t, test.expectedResult, actualResult)
		})
	}
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=false")
}
