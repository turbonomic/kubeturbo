package dtofactory

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"testing"
)

var TestNodes = []struct {
	name    string
	cpuCap  float64
	memCap  float64
	cluster string
}{
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
		//fmt.Printf("%++v\n", kubenode)
		kubenodes = append(kubenodes, kubenode)
	}

	return kubenodes
}

var TestQuotas = []struct {
	name     string
	cpuLimit string
	cpuUsed  string
	memLimit string
	memUsed  string
}{
	{"quota1", "4", "3", "8Gi", "5Gi"},
	{"quota2", "0.5", "0.3", "7Mi", "5Gi"},
	{"quota3", "6000m", "300m", "6Ki", "5Ki"},
}

var TestNodeProviders = []struct {
	node    string
	cpuUsed float64
	memUsed float64
}{
	{"node1", 1.0, 100 * 1024 * 1024.0},
	{"node2", 0.5, 150 * 1024.0 * 1024.0},
	{"node3", 1.5, 250 * 1024.0 * 1024.0},
}

func makeKubeQuotas() []*repository.KubeQuota {
	namespace := "ns1"
	cluster := "cluster1"

	var kubeQuotas []*repository.KubeQuota
	var clusterResources map[metrics.ResourceType]*repository.KubeDiscoveredResource
	for i, testQuota := range TestQuotas {
		uuid := fmt.Sprintf("vdc-%d", i)
		kubeQuota := repository.CreateDefaultQuota(cluster, namespace, uuid, clusterResources)
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuUsed),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memUsed),
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
		//fmt.Printf("%++v\n", quota)
		kubeQuota.ReconcileQuotas(quotaList)

		for _, node := range TestNodes {
			allocationResourceMap := make(map[metrics.ResourceType]float64)
			allocationResourceMap[metrics.CPULimit] = node.cpuCap
			allocationResourceMap[metrics.MemoryLimit] = node.memCap
			kubeQuota.AddNodeProvider(node.name, allocationResourceMap)
		}

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
		}

		commBoughtList := dto.GetCommoditiesBought()
		for _, commBoughtPerProvider := range commBoughtList {
			boughtList := commBoughtPerProvider.GetBought()
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
