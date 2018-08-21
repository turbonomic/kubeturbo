package processor

import (
	"fmt"
	cadvisorapi "github.com/google/cadvisor/info/v1"
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"testing"
	"time"
)

var (
	testClusterName = "TestCluster"
	allocatableMap  = map[v1.ResourceName]resource.Quantity{
		v1.ResourceCPU:    resource.MustParse("4.0"),
		v1.ResourceMemory: resource.MustParse("8010812Ki"),
	}
	allocatableCpuOnlyMap = map[v1.ResourceName]resource.Quantity{
		v1.ResourceCPU: resource.MustParse("4.0"),
	}
	schedulableNodeMap = map[string]bool{
		"node1": false,
		"node2": true,
		"node3": true,
		"node4": true,
	}
	mockNodes = []struct {
		name      string
		ipAddress string
		cpuFreq   uint64
	}{
		{name: "node1",
			ipAddress: "10.10.10.1",
			cpuFreq:   2400000,
		},
		{name: "node2",
			ipAddress: "10.10.10.2",
			cpuFreq:   3200000,
		},
		{name: "node3",
			ipAddress: "10.10.10.3",
			cpuFreq:   2400000,
		},
		{name: "node4",
			ipAddress: "10.10.10.4",
			cpuFreq:   3200000,
		},
	}
)

func createMockNodes(allocatableMap map[v1.ResourceName]resource.Quantity, schedulableNodeMap map[string]bool) []*v1.Node {
	var nodeList []*v1.Node
	for _, mockNode := range mockNodes {
		nodeAddresses := []v1.NodeAddress{{Type: v1.NodeInternalIP, Address: mockNode.ipAddress}}
		conditions := []v1.NodeCondition{{Type: v1.NodeReady, Status: v1.ConditionTrue}}
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: mockNode.name,
				UID:  types.UID(mockNode.name),
			},
			Status: v1.NodeStatus{
				Allocatable: allocatableMap,
				Addresses:   nodeAddresses,
				Conditions:  conditions,
			},
			Spec: v1.NodeSpec{
				Unschedulable: !schedulableNodeMap[mockNode.name],
			},
		}
		nodeList = append(nodeList, node)
	}
	return nodeList
}

func createMockKubeNodes(allocatableMap map[v1.ResourceName]resource.Quantity, schedulableNodeMap map[string]bool) map[string]*repository.KubeNode {
	nodeMap := make(map[string]*repository.KubeNode)
	for _, mockNode := range createMockNodes(allocatableMap, schedulableNodeMap) {
		knode := repository.NewKubeNode(mockNode, testClusterName)
		nodeMap[knode.Name] = knode
	}

	return nodeMap
}

func TestProcessNodes(t *testing.T) {

	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetKubernetesServiceID: func() (string, error) {
			return testClusterName, nil
		},
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
	}

	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
	}
	nodeMap, _ := clusterProcessor.processNodes("TestCluster")
	assert.Equal(t, len(nodeList), len(nodeMap))

	for _, knode := range nodeMap {
		resources := knode.ComputeResources
		assert.Equal(t, len(allocatableMap), len(resources))

		for _, computeType := range metrics.KubeComputeResourceTypes {
			_, exists := resources[computeType]
			assert.True(t, exists)
		}
	}
}

func TestDrainWorkQueue(t *testing.T) {
	size := 2
	work := make(chan *v1.Node, size)
	node := &v1.Node{}
	work <- node
	close(work)
	drainWorkQueue(work)
	_, ok := <-work
	assert.False(t, ok)
}

func TestWaitForCompletionSucceeded(t *testing.T) {
	size := 2
	done := make(chan bool, size)
	done <- true
	totalWaitTime = time.Second
	assert.True(t, waitForCompletion(done))
	totalWaitTime = 60 * time.Second
}

func TestWaitForCompletionFailed(t *testing.T) {
	size := 2
	done := make(chan bool, size)
	totalWaitTime = time.Second
	assert.False(t, waitForCompletion(done))
	totalWaitTime = 60 * time.Second
}

func TestComputeClusterResources(t *testing.T) {
	var resourceMap map[metrics.ResourceType]*repository.KubeDiscoveredResource
	resourceMap = computeClusterResources(createMockKubeNodes(allocatableMap, schedulableNodeMap))
	for _, computeType := range metrics.KubeComputeResourceTypes {
		_, exists := resourceMap[computeType]
		assert.True(t, exists)
	}
	// Among all nodes, one of the nodes is not schedulable, so the capacity should be 3 * node capacity
	assert.Equal(t, int32(24032436), int32(resourceMap["Memory"].Capacity))
	assert.Equal(t, int8(12), int8(resourceMap["CPU"].Capacity))

	resourceMap = computeClusterResources(createMockKubeNodes(allocatableCpuOnlyMap, schedulableNodeMap))
	for _, computeType := range metrics.KubeComputeResourceTypes {
		_, exists := resourceMap[computeType]
		assert.True(t, exists, fmt.Sprintf("missing %s resource", computeType))
	}
}

func TestDiscoverCluster(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetKubernetesServiceID: func() (string, error) {
			return testClusterName, nil
		},
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		validationResult:   &ClusterValidationResult{IsValidated: true},
	}

	testCluster, err := clusterProcessor.DiscoverCluster()
	assert.Nil(t, err)
	nodeMap := testCluster.Nodes
	assert.Equal(t, len(nodeList), len(nodeMap))
	assert.Equal(t, 0, len(testCluster.Namespaces))
}

func TestDiscoverClusterChangedSchedulableNodes(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetKubernetesServiceID: func() (string, error) {
			return testClusterName, nil
		},
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		validationResult:   &ClusterValidationResult{IsValidated: true},
	}

	testCluster1, _ := clusterProcessor.DiscoverCluster()
	nodeMap := testCluster1.Nodes
	assert.Equal(t, len(nodeList), len(nodeMap))
	assert.Equal(t, 0, len(testCluster1.Namespaces))

	for _, node := range nodeMap {
		assert.Equal(t, schedulableNodeMap[node.Name], !node.Node.Spec.Unschedulable)
	}

	newSchedulableNodeMap := map[string]bool{
		"node1": true,
		"node2": false,
		"node3": true,
		"node4": false,
	}
	nodeList = createMockNodes(allocatableMap, newSchedulableNodeMap)
	ms.mockGetAllNodes = func() ([]*v1.Node, error) {
		return nodeList, nil
	}
	testCluster2, _ := clusterProcessor.DiscoverCluster()
	nodeMap = testCluster2.Nodes
	assert.Equal(t, len(nodeList), len(nodeMap))
	for _, node := range nodeMap {
		assert.Equal(t, newSchedulableNodeMap[node.Name], !node.Node.Spec.Unschedulable)
	}
}

func TestConnectCluster(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
		mockGetKubernetesServiceID: func() (svcID string, err error) {
			return testClusterName, nil
		},
	}

	ns := &MockNodeScrapper{
		mockGetMachineCpuFrequency: func(host string) (uint64, error) {
			for _, mockNode := range mockNodes {
				if mockNode.ipAddress == host {
					return mockNode.cpuFreq, nil
				}
			}
			return 0, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	err := clusterProcessor.ConnectCluster()
	//assert.NotNil(t, kubeCluster)
	assert.True(t, clusterProcessor.validationResult.IsValidated)
	assert.Nil(t, err)
}

func TestConnectClusterUnreachableNodes(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
		mockGetKubernetesServiceID: func() (svcID string, err error) {
			return testClusterName, nil
		},
	}

	ns := &MockNodeScrapper{
		mockGetMachineCpuFrequency: func(host string) (uint64, error) {
			return 0, fmt.Errorf("%s node is unreachable", host)
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	err := clusterProcessor.ConnectCluster()
	assert.False(t, clusterProcessor.validationResult.IsValidated)
	assert.NotNil(t, err)
}

func TestConnectClusterReachableAndUnreachableNodes(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
		mockGetKubernetesServiceID: func() (svcID string, err error) {
			return testClusterName, nil
		},
	}

	reachableNodes := []string{"10.10.10.1", "10.10.10.2"}
	ns := &MockNodeScrapper{
		mockGetMachineCpuFrequency: func(host string) (uint64, error) {
			for _, nodeIp := range reachableNodes {
				if nodeIp == host {
					return 6400000, nil
				}
			}
			return 0, fmt.Errorf("%s node is unreachable", host)
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	errors := clusterProcessor.ConnectCluster()
	fmt.Printf("err %s\n", errors)
	//assert.NotNil(t, kubeCluster)
	assert.Nil(t, errors)
}

// Implements the ClusterScrapperInterface.
// Method implementation will check to see if the test has provided the mockXXX method function
type MockClusterScrapper struct {
	mockGetAllNodes            func() ([]*v1.Node, error)
	mockGetNamespaces          func() ([]*v1.Namespace, error)
	mockGetNamespaceQuotas     func() (map[string][]*v1.ResourceQuota, error)
	mockGetAllPods             func() ([]*v1.Pod, error)
	mockGetAllEndpoints        func() ([]*v1.Endpoints, error)
	mockGetAllServices         func() ([]*v1.Service, error)
	mockGetKubernetesServiceID func() (svcID string, err error)
}

func (s *MockClusterScrapper) GetAllNodes() ([]*v1.Node, error) {
	if s.mockGetAllNodes != nil {
		return s.mockGetAllNodes()
	}
	return nil, fmt.Errorf("GetAllNodes Not implemented")
}

func (s *MockClusterScrapper) GetNodes(opts metav1.ListOptions) ([]*v1.Node, error) {
	if s.mockGetAllNodes != nil {
		return s.mockGetAllNodes()
	}
	return nil, fmt.Errorf("GetAllNodes Not implemented")
}

func (s *MockClusterScrapper) GetNamespaces() ([]*v1.Namespace, error) {
	if s.mockGetNamespaces != nil {
		return s.mockGetNamespaces()
	}
	return nil, fmt.Errorf("GetNamespaces Not implemented")
}
func (s *MockClusterScrapper) GetNamespaceQuotas() (map[string][]*v1.ResourceQuota, error) {
	if s.mockGetNamespaceQuotas != nil {
		return s.mockGetNamespaceQuotas()
	}
	return nil, fmt.Errorf("GetNamespaceQuotas Not implemented")
}
func (s *MockClusterScrapper) GetAllPods() ([]*v1.Pod, error) {
	if s.mockGetAllPods != nil {
		return s.mockGetAllPods()
	}
	return nil, fmt.Errorf("GetAllPods Not implemented")
}
func (s *MockClusterScrapper) GetAllEndpoints() ([]*v1.Endpoints, error) {
	if s.mockGetAllEndpoints != nil {
		return s.mockGetAllEndpoints()
	}
	return nil, fmt.Errorf("GetAllEndpoints Not implemented")
}

func (s *MockClusterScrapper) GetKubernetesServiceID() (string, error) {
	if s.mockGetKubernetesServiceID != nil {
		return s.mockGetKubernetesServiceID()
	}
	return "", fmt.Errorf("GetKubernetesServiceID Not implemented")
}
func (s *MockClusterScrapper) GetAllServices() ([]*v1.Service, error) {
	if s.mockGetAllServices != nil {
		return s.mockGetAllServices()
	}
	return nil, fmt.Errorf("GetAllServices Not implemented")
}

// Implements the KubeHttpClientInterface
// Method implementation will check to see if the test has provided the mockXXX method function
type MockNodeScrapper struct {
	mockExecuteRequestAndGetValue func(host string, endpoint string, value interface{}) error
	mockGetSummary                func(host string) (*stats.Summary, error)
	mockGetMachineInfo            func(host string) (*cadvisorapi.MachineInfo, error)
	mockGetMachineCpuFrequency    func(host string) (uint64, error)
}

func (s *MockNodeScrapper) ExecuteRequestAndGetValue(host string, endpoint string, value interface{}) error {
	if s.mockExecuteRequestAndGetValue != nil {
		return s.mockExecuteRequestAndGetValue(host, endpoint, value)
	}
	return fmt.Errorf("ExecuteRequestAndGetValue Not implemented")
}

func (s *MockNodeScrapper) GetSummary(host string) (*stats.Summary, error) {
	if s.mockGetSummary != nil {
		return s.mockGetSummary(host)
	}
	return nil, fmt.Errorf("GetSummary Not implemented")
}

func (s *MockNodeScrapper) GetMachineInfo(host string) (*cadvisorapi.MachineInfo, error) {
	if s.mockGetMachineInfo != nil {
		return s.mockGetMachineInfo(host)
	}
	return nil, fmt.Errorf("GetMachineInfo Not implemented")
}

func (s *MockNodeScrapper) GetMachineCpuFrequency(host string) (uint64, error) {
	if s.mockGetMachineCpuFrequency != nil {
		return s.mockGetMachineCpuFrequency(host)
	}
	return 0, fmt.Errorf("GetMachineCpuFrequency Not implemented")
}
