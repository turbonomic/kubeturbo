package dtofactory

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	v1 "k8s.io/api/core/v1"
	k8sres "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const CPUFrequency float64 = 2663.778000

type TestNode struct {
	name    string
	cpuCap  float64
	memCap  float64
	cluster string
}

const clusterName string = "foo"

var TestNodes = []TestNode{
	{
		"node1",
		4.0,
		819200,
		clusterName,
	},
	{
		"node2",
		5.0,
		614400,
		clusterName,
	},
	{
		"node3",
		6.0,
		409600,
		clusterName,
	},
}

func makeKubeNodes() []*repository.KubeNode {
	var kubenodes []*repository.KubeNode
	for _, testNode := range TestNodes {
		resourceList := v1.ResourceList{
			v1.ResourceCPU:    k8sres.MustParse(fmt.Sprint(testNode.cpuCap)),
			v1.ResourceMemory: k8sres.MustParse(fmt.Sprint(testNode.memCap)),
		}

		n1 := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNode.name,
				UID:  types.UID(testNode.name),
			},
			Status: v1.NodeStatus{
				Allocatable: resourceList,
			},
		}

		n1.Spec.ProviderID = "random.vcenter"
		n1.Status.NodeInfo.SystemUUID = "uuid-" + n1.Name

		kubenode := repository.NewKubeNode(n1, testNode.cluster)
		kubenodes = append(kubenodes, kubenode)
	}

	return kubenodes
}

type TestNamespace struct {
	name            string
	cpuLimitQuota   string
	cpuLimitUsed    string
	memLimitQuota   string
	memLimitUsed    string
	cpuRequestQuota string
	cpuRequestUsed  string
	memRequestQuota string
	memRequestUsed  string
	cpuActualUsed   []float64
	memActualUsed   []float64
}

var TestNamespaces = []TestNamespace{
	{
		"namespace1",
		"4",
		"3",
		"8Gi",
		"5Gi",
		"4",
		"2",
		"6Gi",
		"2Gi",
		[]float64{2.8, 2.1, 2.2, 1.5},
		[]float64{4, 4.8, 4.2, 4.1},
	},
	{
		"namespace2",
		"0.5",
		"0.3",
		"7Mi",
		"7Mi",
		"0.4",
		"0.22",
		"6Mi",
		"2Gi",
		[]float64{0.22},
		[]float64{5},
	},
	{
		"namespace3",
		"6000m",
		"3000m",
		"6Ki",
		"5.4Ki",
		"4000m",
		"250m",
		"6Ki",
		"2Ki",
		[]float64{300, 350},
		[]float64{5, 5.4},
	},
	{
		"namespace4",
		"0",
		"0",
		"6Ki",
		"5.5Ki",
		"0",
		"0",
		"6Ki",
		"1Ki",
		[]float64{0},
		[]float64{5},
	},
}

func makeKubeNamespaces() []*repository.KubeNamespace {
	namespace := "ns1"
	cluster := clusterName

	var kubeQuotas []*repository.KubeNamespace
	clusterCapacityByResource := make(map[metrics.ResourceType]float64)
	for _, node := range TestNodes {
		clusterCapacityByResource[metrics.CPU] = clusterCapacityByResource[metrics.CPU] + node.cpuCap
		clusterCapacityByResource[metrics.Memory] = clusterCapacityByResource[metrics.Memory] + node.memCap
		clusterCapacityByResource[metrics.CPURequest] = clusterCapacityByResource[metrics.CPURequest] + node.cpuCap
		clusterCapacityByResource[metrics.MemoryRequest] = clusterCapacityByResource[metrics.MemoryRequest] + node.memCap
	}
	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for rt, cap := range clusterCapacityByResource {
		r := &repository.KubeDiscoveredResource{
			Type:     rt,
			Capacity: cap,
		}
		clusterResources[rt] = r
	}
	for i, testQuota := range TestNamespaces {
		uuid := fmt.Sprintf("namespace-%d", i)
		kubeNamespace := repository.CreateDefaultKubeNamespace(cluster, namespace, uuid)

		hardResourceList := v1.ResourceList{}
		usedResourceList := v1.ResourceList{}
		if testQuota.cpuLimitQuota != "0" {
			hardResourceList[v1.ResourceLimitsCPU] = k8sres.MustParse(testQuota.cpuLimitQuota)
			usedResourceList[v1.ResourceLimitsCPU] = k8sres.MustParse(testQuota.cpuLimitUsed)
			hardResourceList[v1.ResourceRequestsCPU] = k8sres.MustParse(testQuota.cpuRequestQuota)
			usedResourceList[v1.ResourceRequestsCPU] = k8sres.MustParse(testQuota.cpuRequestUsed)
		}
		if testQuota.memLimitQuota != "0" {
			hardResourceList[v1.ResourceLimitsMemory] = k8sres.MustParse(testQuota.memLimitQuota)
			usedResourceList[v1.ResourceLimitsMemory] = k8sres.MustParse(testQuota.memLimitUsed)
			hardResourceList[v1.ResourceRequestsMemory] = k8sres.MustParse(testQuota.memRequestQuota)
			usedResourceList[v1.ResourceRequestsMemory] = k8sres.MustParse(testQuota.memRequestUsed)
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}

		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)
		kubeNamespace.ReconcileQuotas(quotaList)

		// simulate pod usage values for the allocation resource commodities without quota limit
		for _, resourceType := range metrics.QuotaResources {
			if !kubeNamespace.QuotaDefined[resourceType] {
				resource := kubeNamespace.AllocationResources[resourceType]
				computeType := metrics.QuotaToComputeMap[resourceType]
				resource.Used = clusterResources[computeType].Capacity * 0.1
			}
		}

		for _, node := range TestNodes {
			allocationResourceMap := make(map[metrics.ResourceType]float64)
			allocationResourceMap[metrics.CPULimitQuota] = node.cpuCap * 0.033
			allocationResourceMap[metrics.CPURequestQuota] = node.cpuCap * 0.033
			allocationResourceMap[metrics.MemoryLimitQuota] = node.memCap * 0.033
			allocationResourceMap[metrics.MemoryRequestQuota] = node.memCap * 0.033
		}

		nowUtcSec := time.Now().Unix()
		cpuUsedPoints := make([]metrics.Point, len(testQuota.cpuActualUsed))
		for i, cpuUsed := range testQuota.cpuActualUsed {
			point := metrics.Point{Value: cpuUsed, Timestamp: nowUtcSec}
			cpuUsedPoints[i] = point
		}
		kubeNamespace.ComputeResources[metrics.CPU].Points = cpuUsedPoints

		memUsedPoints := make([]metrics.Point, len(testQuota.memActualUsed))
		for i, memUsed := range testQuota.memActualUsed {
			point := metrics.Point{Value: memUsed, Timestamp: nowUtcSec}
			memUsedPoints[i] = point
		}
		kubeNamespace.ComputeResources[metrics.Memory].Points = memUsedPoints

		kubeNamespace.AverageNodeCpuFrequency = CPUFrequency
		kubeQuotas = append(kubeQuotas, kubeNamespace)
	}

	return kubeQuotas
}

func TestBuildNamespaceDto(t *testing.T) {

	namespaceMap := make(map[string]*repository.KubeNamespace)
	namespaceList := makeKubeNamespaces()
	for _, kubeNamespace := range namespaceList {
		namespaceMap[kubeNamespace.UID] = kubeNamespace
	}

	nodeMapByUID := make(map[string]*repository.KubeNode)
	nodeList := makeKubeNodes()
	for _, kubeNode := range nodeList {
		nodeMapByUID[kubeNode.UID] = kubeNode
	}

	builder := NewNamespaceEntityDTOBuilder(namespaceMap, false)
	dtos, err := builder.BuildEntityDTOs()
	assert.Nil(t, err)

	for _, dto := range dtos {
		// test AggregatedBy relationship
		assert.True(t, len(dto.ConnectedEntities) > 0,
			fmt.Sprintf("Namespace %v should have at least one connected entity - the cluster", dto.DisplayName))
		hasAggregatedBy := false
		for _, connectedEntity := range dto.ConnectedEntities {
			if connectedEntity.GetConnectionType() == proto.ConnectedEntity_AGGREGATED_BY_CONNECTION {
				hasAggregatedBy = true
				assert.Equal(t, clusterName, connectedEntity.GetConnectedEntityId(),
					fmt.Sprintf("Namespace %v must be aggregated by cluster %v but is aggregated by %v instead",
						dto.DisplayName, clusterName, connectedEntity.GetConnectedEntityId()))
			}
		}
		assert.True(t, hasAggregatedBy, fmt.Sprintf("Namespace %v does not have an AggregatedBy connection", dto.DisplayName))

		commSoldList := dto.GetCommoditiesSold()
		for _, commSold := range commSoldList {
			if proto.CommodityDTO_VMPM_ACCESS.String() == commSold.CommodityType.String() {
				assert.EqualValues(t, dto.GetDisplayName(), commSold.GetKey())
			} else {
				assert.EqualValues(t, dto.GetId(), commSold.GetKey())
			}
		}
		commMap := make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)
		for _, commSold := range commSoldList {
			commMap[commSold.GetCommodityType()] = commSold
		}

		// Namespace data
		assert.EqualValues(t, CPUFrequency, *dto.GetNamespaceData().AverageNodeCpuFrequency)

		// source of the commodities
		kubeNamespace, exists := namespaceMap[dto.GetId()]
		assert.True(t, exists)

		for _, allocationResource := range metrics.QuotaResources {
			commType, ok := rTypeMapping[allocationResource]
			if !ok {
				continue
			}
			comm, exists := commMap[commType]
			assert.True(t, exists, fmt.Sprintf("%s does not exist", commType))
			assert.EqualValues(t, dto.GetId(), comm.GetKey())

			resource, exists := kubeNamespace.AllocationResources[allocationResource]
			assert.True(t, exists, fmt.Sprintf("%v does not exist", resource))
			if metrics.IsCPUType(allocationResource) {
				if dto.GetId() == "namespace-3" {
					assert.EqualValues(t, resource.Capacity, comm.GetCapacity())
				} else {
					assert.EqualValues(t, resource.Capacity, comm.GetCapacity())
				}
			} else {
				assert.EqualValues(t, resource.Capacity, comm.GetCapacity())
			}
			if metrics.IsCPUType(allocationResource) && kubeNamespace.QuotaDefined[allocationResource] {
				assert.EqualValues(t, resource.Used, comm.GetUsed())
			}
		}

		commBoughtList := dto.GetCommoditiesBought()
		for _, commBoughtPerProvider := range commBoughtList {
			boughtList := commBoughtPerProvider.GetBought()
			commMap = make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)
			foundClusterCommodity := false
			for _, commBought := range boughtList {
				commType := commBought.GetCommodityType()
				if commType != proto.CommodityDTO_CLUSTER {
					commMap[commType] = commBought
					continue
				}
				// verify the cluster commodity
				foundClusterCommodity = true
				assert.Equal(t, GetClusterKey(clusterName, false), commBought.GetKey())
			}
			assert.True(t, foundClusterCommodity)

			for _, computeResource := range metrics.ComputeResources {
				commType, ok := rTypeMapping[computeResource]
				if !ok {
					continue
				}
				comm, exists := commMap[commType]
				assert.True(t, exists)

				// check and compare with expected values
				expectedResource, exists := kubeNamespace.ComputeResources[computeResource]
				assert.Equal(t, "", comm.GetKey())
				if len(expectedResource.Points) > 0 {
					sum := 0.0
					peak := 0.0
					for _, point := range expectedResource.Points {
						sum += point.Value
						peak = math.Max(peak, point.Value)
					}
					avg := sum / float64(len(expectedResource.Points))
					assert.EqualValues(t, avg, comm.GetUsed())
					assert.EqualValues(t, peak, comm.GetPeak())
				}
			}
		}
	}

	//fmt.Printf("DTOs: %++v\n", dtos)
	assert.EqualValues(t, len(TestNamespaces), len(dtos))
}
