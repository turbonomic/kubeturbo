package dtofactory

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

const CPUFrequency float64 = 2663.778000

type TestNode struct {
	name    string
	cpuCap  float64
	memCap  float64
	cluster string
}

var TestNodes = []TestNode{
	{"node1", 4.0, 819200, "cluster1"},
	{"node2", 5.0, 614400, "cluster1"},
	{"node3", 6.0, 409600, "cluster1"},
}

func makeKubeNodes() []*repository.KubeNode {
	var kubenodes []*repository.KubeNode
	for _, testNode := range TestNodes {
		resourceList := v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(fmt.Sprint(testNode.cpuCap)),
			v1.ResourceMemory: resource.MustParse(fmt.Sprint(testNode.memCap)),
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

type TestQuota struct {
	name     string
	cpuLimit string
	cpuUsed  string
	memLimit string
	memUsed  string
}
type TestQuotaWithMemLimit struct {
	name     string
	memLimit string
	memUsed  string
}

//var TestQuotas = []struct {
//	name     string
//	cpuLimit string
//	cpuUsed  string
//	memLimit string
//	memUsed  string
//}
var TestQuotas = []TestQuota{
	{"quota1", "4", "3", "8Gi", "5Gi"},
	{"quota2", "0.5", "0.3", "7Mi", "5Gi"},
	{"quota3", "6000m", "300m", "6Ki", "5Ki"},
	{"quota4", "0", "0", "6Ki", "5Ki"},
}

var TestQuotasWithMemLimit = []TestQuotaWithMemLimit{
	{"quota1", "8Gi", "5Gi"},
	{"quota2", "7Mi", "5Gi"},
	{"quota3", "6Ki", "5Ki"},
}

//
//var TestNodeProviders = []TestNode {
//	{"node1", 1.0, 100 * 1024 * 1024.0},
//	{"node2", 0.5, 150 * 1024.0 * 1024.0},
//	{"node3", 1.5, 250 * 1024.0 * 1024.0},
//}

func makeKubeQuotas() []*repository.KubeQuota {
	namespace := "ns1"
	cluster := "cluster1"

	var kubeQuotas []*repository.KubeQuota
	resourceMap := make(map[metrics.ResourceType]float64)
	for _, node := range TestNodes {
		resourceMap[metrics.CPU] = resourceMap[metrics.CPU] + node.cpuCap
		resourceMap[metrics.Memory] = resourceMap[metrics.Memory] + node.memCap
	}
	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for rt, resource := range resourceMap {
		r := &repository.KubeDiscoveredResource{
			Type:     rt,
			Capacity: resource,
		}
		clusterResources[rt] = r
	}
	for i, testQuota := range TestQuotas {
		uuid := fmt.Sprintf("vdc-%d", i)
		kubeQuota := repository.CreateDefaultQuota(cluster, namespace, uuid, clusterResources)

		hardResourceList := v1.ResourceList{}
		usedResourceList := v1.ResourceList{}
		if testQuota.cpuLimit != "0" {
			hardResourceList[v1.ResourceLimitsCPU] = resource.MustParse(testQuota.cpuLimit)
			usedResourceList[v1.ResourceLimitsCPU] = resource.MustParse(testQuota.cpuUsed)
		}
		if testQuota.memLimit != "0" {
			hardResourceList[v1.ResourceLimitsMemory] = resource.MustParse(testQuota.memLimit)
			usedResourceList[v1.ResourceLimitsMemory] = resource.MustParse(testQuota.memUsed)
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
		kubeQuota.ReconcileQuotas(quotaList)

		// simulate pod usage values for the allocation resource commodities without quota limit
		for _, resourceType := range metrics.ComputeAllocationResources {
			if !kubeQuota.AllocationDefined[resourceType] {
				resource := kubeQuota.AllocationResources[resourceType]
				computeType := metrics.AllocationToComputeMap[resourceType]
				resource.Used = clusterResources[computeType].Capacity * 0.1
			}
		}

		for _, node := range TestNodes {
			allocationResourceMap := make(map[metrics.ResourceType]float64)
			allocationResourceMap[metrics.CPULimit] = node.cpuCap * 0.033
			allocationResourceMap[metrics.CPURequest] = node.cpuCap * 0.033
			allocationResourceMap[metrics.MemoryLimit] = node.memCap * 0.033
			allocationResourceMap[metrics.MemoryRequest] = node.memCap * 0.033
			kubeQuota.AddNodeProvider(node.name, allocationResourceMap)
		}
		kubeQuota.AverageNodeCpuFrequency = CPUFrequency
		kubeQuotas = append(kubeQuotas, kubeQuota)
	}

	return kubeQuotas
}

func TestBuildQuotaDto(t *testing.T) {

	quotaMap := make(map[string]*repository.KubeQuota)
	quotaList := makeKubeQuotas()
	for _, kubeQuota := range quotaList {
		quotaMap[kubeQuota.UID] = kubeQuota
	}

	nodeMapByUID := make(map[string]*repository.KubeNode)
	nodeList := makeKubeNodes()
	for _, kubeNode := range nodeList {
		nodeMapByUID[kubeNode.UID] = kubeNode
	}

	builder := NewQuotaEntityDTOBuilder(quotaMap, nodeMapByUID, stitching.UUID)
	dtos, err := builder.BuildEntityDTOs()
	assert.Nil(t, err)

	for _, dto := range dtos {
		commSoldList := dto.GetCommoditiesSold()
		for _, commSold := range commSoldList {
			assert.EqualValues(t, dto.GetId(), commSold.GetKey())
		}
		commMap := make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)
		for _, commSold := range commSoldList {
			commMap[commSold.GetCommodityType()] = commSold
		}

		for _, allocationResource := range metrics.ComputeAllocationResources {
			commType, ok := rTypeMapping[allocationResource]
			if !ok {
				continue
			}
			comm, exists := commMap[commType]
			assert.True(t, exists, fmt.Sprintf("%s does not exist", commType))
			assert.EqualValues(t, dto.GetId(), comm.GetKey())
			quota, exists := quotaMap[dto.GetId()]
			assert.True(t, exists)

			resource, exists := quota.AllocationResources[allocationResource]
			assert.True(t, exists, fmt.Sprintf("%v does not exist", resource))
			if metrics.IsCPUType(allocationResource) {
				assert.EqualValues(t, resource.Capacity*CPUFrequency, comm.GetCapacity())
			} else {
				assert.EqualValues(t, resource.Capacity, comm.GetCapacity())
			}
			if metrics.IsCPUType(allocationResource) && quota.AllocationDefined[allocationResource] {
				assert.EqualValues(t, resource.Used*CPUFrequency, comm.GetUsed())
			}
		}

		commBoughtList := dto.GetCommoditiesBought()
		providerMap := make(map[string]TestNode)
		for _, node := range TestNodes {
			providerMap[node.name] = node
		}
		for _, commBoughtPerProvider := range commBoughtList {
			boughtList := commBoughtPerProvider.GetBought()
			provider := *commBoughtPerProvider.ProviderId
			_, exists := providerMap[provider]
			assert.True(t, exists, fmt.Sprintf("%s provider does not exist", provider))
			commMap = make(map[proto.CommodityDTO_CommodityType]*proto.CommodityDTO)
			for _, commBought := range boughtList {
				commMap[commBought.GetCommodityType()] = commBought
			}

			for _, allocationResource := range metrics.ComputeAllocationResources {
				commType, ok := rTypeMapping[allocationResource]
				if !ok {
					continue
				}
				comm, exists := commMap[commType]
				assert.True(t, exists)
				assert.EqualValues(t, *commBoughtPerProvider.ProviderId, comm.GetKey())
			}
		}
	}

	//fmt.Printf("DTOs: %++v\n", dtos)
	assert.EqualValues(t, len(TestQuotas), len(dtos))
}
