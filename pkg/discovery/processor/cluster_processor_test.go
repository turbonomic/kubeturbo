package processor

import (
	"fmt"
	"testing"
	"time"

	cadvisorapi "github.com/google/cadvisor/info/v1"
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	gitopsv1alpha1 "github.com/turbonomic/turbo-gitops/api/v1alpha1"
	policyv1alpha1 "github.com/turbonomic/turbo-policy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
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

func TestDiscoverCluster(t *testing.T) {
	nodeList := createMockNodes(allocatableMap, schedulableNodeMap)
	ms := &MockClusterScrapper{
		mockGetKubernetesServiceID: func() (string, error) {
			return testClusterName, nil
		},
		mockGetAllNodes: func() ([]*v1.Node, error) {
			return nodeList, nil
		},
		mockGetAllPods: func() ([]*v1.Pod, error) {
			return []*v1.Pod{}, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		isValidated:        true,
	}

	testCluster, err := clusterProcessor.DiscoverCluster()
	assert.Nil(t, err)
	nodeMap := testCluster.NodeMap
	assert.Equal(t, len(nodeList), len(nodeMap))
	assert.Equal(t, 0, len(testCluster.NamespaceMap))
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
		mockGetAllPods: func() ([]*v1.Pod, error) {
			return []*v1.Pod{}, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		isValidated:        true,
	}

	testCluster1, _ := clusterProcessor.DiscoverCluster()
	nodeMap := testCluster1.NodeMap
	assert.Equal(t, len(nodeList), len(nodeMap))
	assert.Equal(t, 0, len(testCluster1.NamespaceMap))

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
	nodeMap = testCluster2.NodeMap
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
		mockGetSummary: func(ip, nodeName string) (*stats.Summary, error) {
			summary := stats.Summary{}
			return &summary, nil
		},
	}
	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	err := clusterProcessor.ConnectCluster()
	//assert.NotNil(t, kubeCluster)
	assert.True(t, clusterProcessor.isValidated)
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
		mockGetSummary: func(ip, nodeName string) (*stats.Summary, error) {
			return nil, fmt.Errorf("%s node is unreachable", ip)
		},
	}

	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	err := clusterProcessor.ConnectCluster()
	assert.False(t, clusterProcessor.isValidated)
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

	ns := &MockNodeScrapper{
		mockGetSummary: func(ip, nodeName string) (*stats.Summary, error) {
			summary := stats.Summary{}
			return &summary, nil
		},
	}

	clusterProcessor := &ClusterProcessor{
		clusterInfoScraper: ms,
		nodeScrapper:       ns,
	}
	errors := clusterProcessor.ConnectCluster()
	fmt.Printf("err %v\n", errors)
	//assert.NotNil(t, kubeCluster)
	assert.Nil(t, errors)
}

// Implements the ClusterScrapperInterface.
// Method implementation will check to see if the test has provided the mockXXX method function
type MockClusterScrapper struct {
	mockGetAllNodes                func() ([]*v1.Node, error)
	mockGetNamespaces              func() ([]*v1.Namespace, error)
	mockGetNamespaceQuotas         func() (map[string][]*v1.ResourceQuota, error)
	mockGetAllPods                 func() ([]*v1.Pod, error)
	mockGetAllEndpoints            func() ([]*v1.Endpoints, error)
	mockGetAllServices             func() ([]*v1.Service, error)
	mockGetKubernetesServiceID     func() (svcID string, err error)
	mockGetAllPVs                  func() ([]*v1.PersistentVolume, error)
	mockGetAllPVCs                 func() ([]*v1.PersistentVolumeClaim, error)
	mockGetAllTurboSLOScalings     func() ([]policyv1alpha1.SLOHorizontalScale, error)
	mockGetAllTurboPolicyBindings  func() ([]policyv1alpha1.PolicyBinding, error)
	mockGetAllGitOpsConfigurations func() ([]gitopsv1alpha1.GitOps, error)
	mockUpdateGitOpsConfigCache    func()
}

func (s *MockClusterScrapper) GetAllTurboSLOScalings() ([]policyv1alpha1.SLOHorizontalScale, error) {
	if s.mockGetAllTurboSLOScalings != nil {
		return s.mockGetAllTurboSLOScalings()
	}
	return nil, fmt.Errorf("GetAllTurboSLOScalings Not implemented")
}

func (s *MockClusterScrapper) GetAllTurboPolicyBindings() ([]policyv1alpha1.PolicyBinding, error) {
	if s.mockGetAllTurboPolicyBindings != nil {
		return s.mockGetAllTurboPolicyBindings()
	}
	return nil, fmt.Errorf("GetAllTurboPolicyBindings Not implemented")
}

func (s *MockClusterScrapper) GetAllGitOpsConfigurations() ([]gitopsv1alpha1.GitOps, error) {
	if s.mockGetAllGitOpsConfigurations != nil {
		return s.mockGetAllGitOpsConfigurations()
	}
	return nil, fmt.Errorf("GetAllGitOpsConfigurations Not implemented")
}

func (s *MockClusterScrapper) UpdateGitOpsConfigCache() {
	if s.mockUpdateGitOpsConfigCache != nil {
		s.mockUpdateGitOpsConfigCache()
	}
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

func (s *MockClusterScrapper) GetAllPVs() ([]*v1.PersistentVolume, error) {
	if s.mockGetAllPVs != nil {
		return s.mockGetAllPVs()
	}
	return nil, fmt.Errorf("GetAllPVs Not implemented")
}

func (s *MockClusterScrapper) GetAllPVCs() ([]*v1.PersistentVolumeClaim, error) {
	if s.mockGetAllPVCs != nil {
		return s.mockGetAllPVCs()
	}
	return nil, fmt.Errorf("GetAllPVCs Not implemented")
}

func (s *MockClusterScrapper) GetResources(schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	return []unstructured.Unstructured{}, nil
}

func (s *MockClusterScrapper) GetResourcesPaginated(schema.GroupVersionResource, int) ([]unstructured.Unstructured, error) {
	return []unstructured.Unstructured{}, nil
}

func (s *MockClusterScrapper) GetMachineSetToNodesMap(nodes []*v1.Node) map[string][]*v1.Node {
	return make(map[string][]*v1.Node)
}

// Implements the KubeHttpClientInterface
// Method implementation will check to see if the test has provided the mockXXX method function
type MockNodeScrapper struct {
	mockExecuteRequest      func(ip, nodeName, path string) ([]byte, error)
	mockGetSummary          func(ip, nodeName string) (*stats.Summary, error)
	mockGetMachineInfo      func(ip, nodeName string) (*cadvisorapi.MachineInfo, error)
	mockGetNodeCpuFrequency func(node *v1.Node) (float64, error)
}

func (s *MockNodeScrapper) ExecuteRequest(ip, nodeName, path string) ([]byte, error) {
	if s.mockExecuteRequest != nil {
		return s.mockExecuteRequest(ip, "", path)
	}
	return nil, fmt.Errorf("ExecuteRequest Not implemented")
}

func (s *MockNodeScrapper) GetSummary(ip, nodeName string) (*stats.Summary, error) {
	if s.mockGetSummary != nil {
		return s.mockGetSummary(ip, nodeName)
	}
	return nil, fmt.Errorf("GetSummary Not implemented")
}

func (s *MockNodeScrapper) GetMachineInfo(ip, nodeName string) (*cadvisorapi.MachineInfo, error) {
	if s.mockGetMachineInfo != nil {
		return s.mockGetMachineInfo(ip, nodeName)
	}
	return nil, fmt.Errorf("GetMachineInfo Not implemented")
}

func (s *MockNodeScrapper) GetNodeCpuFrequency(node *v1.Node) (float64, error) {
	if s.mockGetNodeCpuFrequency != nil {
		return s.mockGetNodeCpuFrequency(node)
	}
	return 0, fmt.Errorf("GetNodeCpuFrequency Not implemented")
}
