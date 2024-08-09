package dtofactory

import (
	"fmt"
	"testing"

	"github.com/golang/glog"
	"github.com/stretchr/testify/assert"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/util/feature"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	label1Key    = "label1"
	label1Value  = "value1"
	label2Key    = "label2"
	label2Value  = "value2"
	taintAKey    = "taintA"
	taintAEffect = api.TaintEffectNoSchedule
	taintBKey    = "taintB"
	taintBValue  = "foo"
	taintBEffect = api.TaintEffectNoExecute
	taintCKey    = "taintC"
	taintCValue  = "bar"
	taintCEffect = api.TaintEffectPreferNoSchedule
)

func TestQuantity(t *testing.T) {
	q1 := resource.NewQuantity(1000, resource.DecimalSI)
	q2 := resource.NewQuantity(1024, resource.BinarySI)

	fmt.Printf("q1 = %++v\n", q1)
	fmt.Printf("q2 = %++v\n", q2)
}

func TestCPUQuantity(t *testing.T) {
	cpuTime1 := "1000m"
	cpuTime2 := "2500m"

	r1, err := resource.ParseQuantity(cpuTime1)
	if err != nil {
		t.Error(err)
		return
	}
	glog.V(1).Infof("cputime1(%s): %++v", cpuTime1, r1)

	r2, err := resource.ParseQuantity(cpuTime2)
	if err != nil {
		t.Error(err)
		return
	}
	glog.V(1).Infof("cputime2(%s): %++v", cpuTime2, r2)
}

func genMemQuantity(numbytes int64) resource.Quantity {
	numkb := int(numbytes / 1024.0)
	result, err := resource.ParseQuantity(fmt.Sprintf("%dKi", numkb))
	if err != nil {
		glog.Errorf("Failed to parse memory quantity: %v", err)
		result = *resource.NewQuantity(1024, resource.BinarySI)
	}
	glog.V(3).Infof("result = %+v", result)

	return result
}

// input: cores--cpu cores
func genCPUQuantity(cores float32) resource.Quantity {
	cpuTime := int(cores * 1000)
	result, err := resource.ParseQuantity(fmt.Sprintf("%dm", cpuTime))
	if err != nil {
		glog.Errorf("Failed to parse cpu quantity: %v", err)
		result = *resource.NewQuantity(1000, resource.DecimalSI)
	}

	glog.V(3).Infof("result = %+v", result)
	return result
}

func buildNodeResource() (api.ResourceList, api.ResourceList) {
	capacity := make(api.ResourceList)
	allocatable := make(api.ResourceList)

	//2 cpu cores, 1.5 are allocatable
	capacity[api.ResourceCPU] = genCPUQuantity(2.0)
	allocatable[api.ResourceCPU] = genCPUQuantity(1.5)

	// 8 GB memory, 6GB are allocatable
	capacity[api.ResourceMemory] = genMemQuantity(8 * 1024 * 1024 * 1024)
	allocatable[api.ResourceMemory] = genMemQuantity(6 * 1024 * 1024 * 1024)

	return capacity, allocatable
}

func genNodeInfo() api.NodeSystemInfo {
	nodeInfo := api.NodeSystemInfo{
		MachineID:        "e414b629ea12ffdaaa044b892dd35750",
		SystemUUID:       "E414B629-EA12-FFDA-AA04-4B892DD35750",
		OSImage:          "Ubuntu 16.04.3 LTS",
		KubeletVersion:   "v1.7.8",
		KubeProxyVersion: "v1.7.8",
		OperatingSystem:  "linux",
		Architecture:     "amd64",
	}
	return nodeInfo
}

func genAddresses() []api.NodeAddress {
	addresses := []api.NodeAddress{
		api.NodeAddress{
			Type:    api.NodeExternalIP,
			Address: "32.205.107.22",
		},
		api.NodeAddress{
			Type:    api.NodeInternalIP,
			Address: "10.10.172.235",
		},
	}

	return addresses
}

func mockNode() *api.Node {
	labels := make(map[string]string)
	labels[label1Key] = label1Value
	labels[label2Key] = label2Value

	resCapaicty, resAllocatable := buildNodeResource()
	addresses := genAddresses()
	nodeInfo := genNodeInfo()

	node := &api.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-node-1",
			UID:    "my-node-1-UID",
			Labels: labels,
		},

		Spec: api.NodeSpec{
			//ExternalID: "2272335446120646149",
			PodCIDR:    "10.4.1.0/24",
			ProviderID: "gce://turbonomic-eng/us-central1-a/gke-cluster-default-pool-b5fbbce4-1ckk",
			Taints: []api.Taint{
				{
					Key:    taintAKey,
					Effect: taintAEffect,
				},
				{
					Key:    taintBKey,
					Value:  taintBValue,
					Effect: taintBEffect,
				},
				{
					Key:    taintCKey,
					Value:  taintCValue,
					Effect: taintCEffect,
				},
			},
		},

		Status: api.NodeStatus{
			Capacity:    resCapaicty,
			Allocatable: resAllocatable,
			Addresses:   addresses,
			NodeInfo:    nodeInfo,
		},
	}

	return node
}

func TestGetNodeIPs(t *testing.T) {
	node := mockNode()

	nodeIPs := getNodeIPs(node)
	filter := make(map[string]struct{})
	for _, ip := range nodeIPs {
		filter[ip] = struct{}{}
	}

	addresses := node.Status.Addresses
	if len(nodeIPs) != len(addresses) {
		t.Errorf("number of IPs are not equal: %d Vs. %d", len(nodeIPs), len(addresses))
		return
	}

	for _, addr := range addresses {
		if _, exist := filter[addr.Address]; !exist {
			t.Errorf("not found address: %+v", addr)
		}
	}
}

func Test_NodeEntityDTO(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("NewAffinityProcessing=true")

	node := mockNode()
	nodeKey := util.NodeKeyFunc(node)
	metricsSink = metrics.NewEntityMetricSink()
	cpuCapMetric := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKey, metrics.CPU, metrics.Capacity, float64(10000))
	metricsSink.AddNewMetricEntries(cpuCapMetric)

	cpuFrequency := metrics.NewEntityStateMetric(metrics.NodeType, nodeKey, metrics.CpuFrequency, float64(2048))
	metricsSink.AddNewMetricEntries(cpuFrequency)

	vstorageAvailableRootfs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKey, metrics.VStorage, metrics.Available, float64(2048000))
	vstorageThresholdRootfs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKey, metrics.VStorage, metrics.Threshold, float64(15))
	vstorageCapacityRootfs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKey, metrics.VStorage, metrics.Capacity, float64(4096000))

	nodeKeyImagefs := nodeKey + "-imagefs"
	vstorageAvailableImagefs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKeyImagefs, metrics.VStorage, metrics.Available, float64(2048000))
	vstorageThresholdImagefs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKeyImagefs, metrics.VStorage, metrics.Threshold, float64(18))
	vstorageCapacityImagefs := metrics.NewEntityResourceMetric(metrics.NodeType, nodeKeyImagefs, metrics.VStorage, metrics.Capacity, float64(4096000))

	metricsSink.AddNewMetricEntries(vstorageAvailableRootfs, vstorageCapacityRootfs, vstorageThresholdRootfs)
	metricsSink.AddNewMetricEntries(vstorageAvailableImagefs, vstorageCapacityImagefs, vstorageThresholdImagefs)

	kubernetesSvcID := "abcdef"
	clusterInfo := metrics.NewEntityStateMetric(metrics.ClusterType, "", metrics.Cluster, kubernetesSvcID)
	metricsSink.AddNewMetricEntries(clusterInfo)
	stitchingManager := stitching.NewStitchingManager(stitching.UUID)
	stitchingManager.StoreStitchingValue(node)
	am := podaffinity.NewAffinityMapper(nil, nil)
	nodeEntityDTOBuilder := NewNodeEntityDTOBuilder(metricsSink, stitchingManager, mockClusterSummaryWithClusterIdOnly(kubernetesSvcID), am)
	pods := []string{"pod1", "pod2"}
	nodePods := map[string][]string{node.Name: pods}
	nodeEntityDTOs, _ := nodeEntityDTOBuilder.BuildEntityDTOs([]*api.Node{node}, nodePods, nil, nil, nil, nil, nil)
	vmData := nodeEntityDTOs[0].GetVirtualMachineData()
	// The capacity metric is set in millicores but numcpus is set in cores
	assert.EqualValues(t, 10, vmData.GetNumCpus())

	// Confirm entity properties are populated and populated properly
	matches := 0
	for _, p := range nodeEntityDTOs[0].GetEntityProperties() {
		if p.GetNamespace() == property.VCTagsPropertyNamespace {
			var expected string
			switch p.GetName() {
			case property.LabelPropertyNamePrefix + " " + label1Key:
				expected = label1Value
			case property.LabelPropertyNamePrefix + " " + label2Key:
				expected = label2Value
			case property.TaintPropertyNamePrefix + " " + string(taintAEffect):
				expected = taintAKey
			case property.TaintPropertyNamePrefix + " " + string(taintBEffect):
				expected = taintBKey + "=" + taintBValue
			case property.TaintPropertyNamePrefix + " " + string(taintCEffect):
				expected = taintCKey + "=" + taintCValue
			default:
				continue
			}
			matches++
			assert.EqualValues(t, expected, p.GetValue())
		}
	}
	assert.EqualValues(t, 5, matches, "there should be 5 tag properties")
	assert.NotEmpty(t, nodeEntityDTOs[0].CommoditiesSold)

	for _, pod := range pods {
		commodityFound := false
		for _, commodity := range nodeEntityDTOs[0].CommoditiesSold {
			if *commodity.Key == pod {
				commodityFound = true
				assert.Equal(t, proto.CommodityDTO_LABEL, *commodity.CommodityType)
				break
			}
		}
		if !commodityFound {
			assert.Fail(t, fmt.Sprintf("Failed to find commodity sold for pod %s", pod))
		}
	}

	// Confirm that if imagefs/rootfs on same partition, that the lowest threshold is selected
	vstorageResourceExists := false
	for _, commodity := range nodeEntityDTOs[0].CommoditiesSold {
		if *commodity.Key == "k8s-node-rootfs" {
			vstorageResourceExists = true
			if *commodity.UtilizationThresholdPct != float64(82) {
				assert.Fail(t, fmt.Sprintf("Utilization Threshold is %.2f, should be 82", *commodity.UtilizationThresholdPct))
			}
		}
	}
	if !vstorageResourceExists {
		assert.Fail(t, "Failed to find VStorage Resource Commodity [rootfs]")
	}
}

func Test_getAffinityCommoditiesSold(t *testing.T) {
	node := mockNode()
	stitchingManager := stitching.NewStitchingManager(stitching.UUID)
	stitchingManager.StoreStitchingValue(node)
	am := podaffinity.NewAffinityMapper(nil, nil)
	nodeEntityDTOBuilder := NewNodeEntityDTOBuilder(metricsSink, stitchingManager,
		mockClusterSummaryWithClusterIdOnly(clusterId), am)
	pod := "test"
	mockNodesPods := make(map[string][]string)
	mockNodesPods[node.Name] = append(mockNodesPods[node.Name], pod)

	commodities := nodeEntityDTOBuilder.getAffinityLabelAndSegmentationComms(node, mockNodesPods, nil, nil, nil)

	assert.NotEmpty(t, commodities)
	assert.Equal(t, 1, len(commodities))
	assert.Equal(t, proto.CommodityDTO_LABEL, *commodities[0].CommodityType)
	assert.Equal(t, pod, *commodities[0].Key)
}

func Test_getSuspendProvisionSettingByNodeType(t *testing.T) {
	node1 := mockNode()
	labels1 := map[string]string{"[k8s label] beta.kubernetes.io/os": "windows"}
	node1.SetLabels(labels1)
	stitchingManager1 := stitching.NewStitchingManager(stitching.UUID)
	stitchingManager1.StoreStitchingValue(node1)
	am := podaffinity.NewAffinityMapper(nil, nil)
	nodeEntityDTOBuilder1 := NewNodeEntityDTOBuilder(metricsSink, stitchingManager1, mockClusterSummaryWithClusterIdOnly(clusterId), am)
	properties1, _ := nodeEntityDTOBuilder1.getNodeProperties(node1)

	node2 := mockNode()
	labels2 := map[string]string{"[k8s label] eks.amazonaws.com/capacityType": "SPOT"}
	node2.SetLabels(labels2)
	stitchingManager2 := stitching.NewStitchingManager(stitching.UUID)
	stitchingManager2.StoreStitchingValue(node2)
	nodeEntityDTOBuilder2 := NewNodeEntityDTOBuilder(metricsSink, stitchingManager2, mockClusterSummaryWithClusterIdOnly(clusterId), am)
	properties2, _ := nodeEntityDTOBuilder2.getNodeProperties(node2)

	node3 := mockNode()
	stitchingManager3 := stitching.NewStitchingManager(stitching.UUID)
	stitchingManager2.StoreStitchingValue(node3)
	nodeEntityDTOBuilder3 := NewNodeEntityDTOBuilder(metricsSink, stitchingManager3, mockClusterSummaryWithClusterIdOnly(clusterId), am)
	properties3, _ := nodeEntityDTOBuilder3.getNodeProperties(node3)

	type args struct {
		properties []*proto.EntityDTO_EntityProperty
	}
	tests := []struct {
		name                        string
		args                        args
		wantDisableSuspendProvision bool
		wantNodeType                string
	}{
		{
			name: "test-with-windows-node",
			args: args{
				properties: properties1,
			},
			wantDisableSuspendProvision: true,
			wantNodeType:                "node with Windows OS",
		},
		{
			name: "test-with-spot-node",
			args: args{
				properties: properties2,
			},
			wantDisableSuspendProvision: true,
			wantNodeType:                "AWS EC2 spot instance",
		},
		{
			name: "test-with-node",
			args: args{
				properties: properties3,
			},
			wantDisableSuspendProvision: false,
			wantNodeType:                "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDisableSuspendProvision, gotNodeType := getSuspendProvisionSettingByNodeType(tt.args.properties)
			if gotDisableSuspendProvision != tt.wantDisableSuspendProvision {
				t.Errorf("getSuspendProvisionSettingByNodeType() gotDisableSuspendProvision = %v, want %v", gotDisableSuspendProvision, tt.wantDisableSuspendProvision)
			}
			if gotNodeType != tt.wantNodeType {
				t.Errorf("getSuspendProvisionSettingByNodeType() gotNodeType = %v, want %v", gotNodeType, tt.wantNodeType)
			}
		})
	}
}

func Test_getAllWorkloadsOnNode(t *testing.T) {
	node1Name := "node1"
	node1 := &api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1Name,
			UID:  types.UID(node1Name),
		},
	}

	node2Name := "node2"
	node2 := &api.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2Name,
			UID:  types.UID(node2Name),
		},
	}

	namespace = "test_namespace"
	pod1 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
		},
		Spec: api.PodSpec{
			NodeName: node1Name,
		},
	}
	pod2 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod2",
		},
		Spec: api.PodSpec{
			NodeName: node1Name,
		},
	}
	pod3 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod3",
		},
		Spec: api.PodSpec{
			NodeName: node2Name,
		},
	}
	pod4 := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod4",
		},
		Spec: api.PodSpec{
			NodeName: node1Name,
		},
	}

	clusterName := "Test_getAllWorkloadsOnNode"
	kubeCluster := repository.NewKubeCluster(clusterName, []*api.Node{node1, node2})

	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	clusterSummary.NodeToRunningPods[node1.Name] = []*api.Pod{pod1, pod2}
	clusterSummary.NodeToRunningPods[node2.Name] = []*api.Pod{pod3}
	clusterSummary.NodeToPendingPods[node1.Name] = []*api.Pod{pod4}

	clusterSummary.PodToControllerMap = make(map[string]string)
	clusterSummary.PodToControllerMap[namespace+"/pod1"] = "controller1"
	clusterSummary.PodToControllerMap[namespace+"/pod2"] = "controller2"
	clusterSummary.PodToControllerMap[namespace+"/pod3"] = "controller3"
	clusterSummary.PodToControllerMap[namespace+"/pod4"] = "controller4"

	workloads := getAllWorkloadsOnNode(node1, clusterSummary)
	expected := sets.NewString("controller1", "controller2", "controller4")
	assert.Equal(t, expected, workloads)
}

func Test_getNodeCommoditiesSold(t *testing.T) {
	feature.DefaultMutableFeatureGate.Set("PeerToPeerAffinityAntiaffinity=true")

	table := []struct {
		topologyKey string
		nodes       []*api.Node
		pods        []*api.Pod
		expectation []struct {
			key           string
			used          float64
			commodityType proto.CommodityDTO_CommodityType
		}
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
						UID: "node0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone": "zone1",
						},
						UID: "node1",
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
			expectation: []struct {
				key           string
				used          float64
				commodityType proto.CommodityDTO_CommodityType
			}{},
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
						UID: "node0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node1",
						},
						UID: "node1",
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
			expectation: []struct {
				key           string
				used          float64
				commodityType proto.CommodityDTO_CommodityType
			}{
				{
					key:           "kubernetes.io/hostname|testns/webserver|testns/db",
					used:          podaffinity.NONE,
					commodityType: proto.CommodityDTO_PEER_TO_PEER_AFFINITY,
				},
				{
					key:           "kubernetes.io/hostname|testns/webserver|testns/db",
					used:          1,
					commodityType: proto.CommodityDTO_PEER_TO_PEER_AFFINITY,
				},
			},
		},
	}

	for _, item := range table {
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		nodeInfoListener := podaffinity.NewNodeInfoLister(clusterSummary)

		srcPodInfo, _ := podaffinity.NewPodInfo(item.pods[0])
		dstPodInfo, _ := podaffinity.NewPodInfo(item.pods[1])
		dstNode := item.nodes[1]

		affinityMapper := podaffinity.NewAffinityMapper(clusterSummary.PodToControllerMap, nodeInfoListener)
		affinityMapper.BuildAffinityMaps(srcPodInfo.RequiredAffinityTerms, srcPodInfo, dstPodInfo, dstNode, podaffinity.Affinity)

		for i, node := range item.nodes {
			nodeKey := util.NodeKeyFunc(node)
			metricsSink = metrics.NewEntityMetricSink()
			cpuFrequency := metrics.NewEntityStateMetric(metrics.NodeType, nodeKey, metrics.CpuFrequency, float64(2048))
			metricsSink.AddNewMetricEntries(cpuFrequency)

			stitchingManager := stitching.NewStitchingManager(stitching.UUID)

			nodeEntityDTOBuilder := NewNodeEntityDTOBuilder(metricsSink, stitchingManager, nil, affinityMapper)
			commodities, _, err := nodeEntityDTOBuilder.getNodeCommoditiesSold(node, kubeCluster.Name)
			assert.True(t, err == nil)

			if len(item.expectation) == 0 {
				continue
			}

			expectation := item.expectation[i]
			var targetCommodity *proto.CommodityDTO
			for _, commodity := range commodities {
				if expectation.key == *commodity.Key {
					targetCommodity = commodity
					break
				}
			}

			assert.True(t, targetCommodity != nil)
			assert.True(t, *targetCommodity.CommodityType == expectation.commodityType)
			assert.True(t, *targetCommodity.Used == expectation.used)
			assert.True(t, *targetCommodity.Capacity == affinityCommodityDefaultCapacity)
		}
	}
}
