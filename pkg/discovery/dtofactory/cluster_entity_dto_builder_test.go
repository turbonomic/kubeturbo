package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	targetId  = "foo"
	clusterId = "bar"
	delta     = 0.000001
)

type Node struct {
	id                 string
	name               string
	numPods            int
	maxPods            int
	cpuCap             float64
	cpuAllocatable     float64
	cpuRequestUsed     float64
	cpuUsed            float64
	memCap             float64
	memAllocatable     float64
	memRequestUsed     float64
	memUsed            float64
	storageCap         float64
	storageAllocatable float64 // not collected yet
	storageRequestUsed float64 // not collected yet
	storageUsed        float64
	powerState         proto.EntityDTO_PowerState
}

var (
	Nodes = []Node{
		{
			"node1id", "node1", 10, 110,
			2.0, 1.9, 1.0, 0.3,
			8168868, 8066468, 140000, 3112000,
			40470, 37297, 0, 9385,
			proto.EntityDTO_POWERED_ON,
		},
		{
			"node2id", "node2", 22, 110,
			2.0, 1.9, 0.89, 1.5,
			8168868, 8066468, 2552000, 1234000,
			40470, 37297, 0, 23748,
			proto.EntityDTO_POWERED_ON,
		},
		{
			"node3id", "node3", 16, 110,
			2.0, 1.9, 0.69, 1.1,
			8168868, 8066468, 3600000, 4164400,
			40470, 37297, 1234, 5678,
			proto.EntityDTO_POWERED_ON,
		},
		{
			"node4id", "powerStateUnknownNode4", 16, 110,
			2.0, 1.9, 0.69, 1.1,
			8168868, 8066468, 3600000, 4164400,
			40470, 37297, 1234, 5678,
			proto.EntityDTO_POWERSTATE_UNKNOWN,
		},
	}
	nsVcpuLimitQuotaUsage1 = float64(5)
	nsVmemLimitQuotaUsage1 = float64(1)
	namespaceDTO1          = &proto.EntityDTO{
		CommoditiesSold: []*proto.CommodityDTO{
			createNSCommodityDTO(proto.CommodityDTO_VCPU_LIMIT_QUOTA, nsVcpuLimitQuotaUsage1),
			createNSCommodityDTO(proto.CommodityDTO_VMEM_LIMIT_QUOTA, nsVmemLimitQuotaUsage1),
		},
	}

	nsVCpuLimitQuotaUsage2 = float64(4)
	nsVMemLimitQuotaUsage2 = float64(2)
	namespaceDTO2          = &proto.EntityDTO{
		CommoditiesSold: []*proto.CommodityDTO{
			createNSCommodityDTO(proto.CommodityDTO_VCPU_LIMIT_QUOTA, nsVCpuLimitQuotaUsage2),
			createNSCommodityDTO(proto.CommodityDTO_VMEM_LIMIT_QUOTA, nsVMemLimitQuotaUsage2),
		},
	}

	nodeVCpuCap = float64(5)
	nodeVMemCap = float64(5)
)

func makeNodeDTOs() ([]*proto.EntityDTO, error) {
	var nodeDTOs []*proto.EntityDTO
	for _, node := range Nodes {
		nodeDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, node.id)
		nodeDTOBuilder.WithPowerState(node.powerState)
		cpu, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU).
			Capacity(node.cpuCap).Used(node.cpuUsed).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(cpu)
		cpuRequest, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VCPU_REQUEST).
			Capacity(node.cpuAllocatable).Used(node.cpuRequestUsed).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(cpuRequest)
		mem, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM).
			Capacity(node.memCap).Used(node.memUsed).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(mem)
		memRequest, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMEM_REQUEST).
			Capacity(node.memAllocatable).Used(node.memRequestUsed).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(memRequest)
		storage, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VSTORAGE).
			Capacity(node.storageCap).Used(node.storageUsed).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(storage)
		numConsumers, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_NUMBER_CONSUMERS).
			Capacity(float64(node.maxPods)).Used(float64(node.numPods)).Create()
		if err != nil {
			return nil, err
		}
		nodeDTOBuilder.SellsCommodity(numConsumers)
		nodeDTO, err := nodeDTOBuilder.Create()
		if err != nil {
			return nil, err
		}
		nodeDTOs = append(nodeDTOs, nodeDTO)
	}
	return nodeDTOs, nil
}

func TestBuildClusterDto(t *testing.T) {
	kubeCluster := repository.KubeCluster{Name: clusterId}
	clusterSummary := repository.ClusterSummary{KubeCluster: &kubeCluster}
	builder := NewClusterDTOBuilder(&clusterSummary, targetId)
	entityDTOs, err := makeNodeDTOs()
	assert.Nil(t, err, "Failed to make node DTOs to build the cluster DTO: %s", err)
	clusterDTO, err := builder.BuildEntity(entityDTOs, entityDTOs)
	assert.Nil(t, err)
	// Cluster commodities will be aggregated from "node1", "node2" and "node3" which have POWERED_ON.
	// Node "powerStateUnknownNode4" is skipped because it has POWERSTATE_UNKNOWN.
	for _, commSold := range clusterDTO.CommoditiesSold {
		switch commSold.GetCommodityType() {
		case proto.CommodityDTO_CLUSTER:
			assert.Equal(t, GetClusterKey(clusterId), commSold.GetKey())
			assert.Equal(t, accessCommodityDefaultCapacity, commSold.GetCapacity())
		case proto.CommodityDTO_NUMBER_CONSUMERS:
			assert.InDelta(t, 10+22+16, commSold.GetUsed(), delta)
			assert.InDelta(t, 10+22+16, commSold.GetPeak(), delta)
			assert.InDelta(t, 110+110+110, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		case proto.CommodityDTO_VCPU:
			assert.InDelta(t, 0.3+1.5+1.1, commSold.GetUsed(), delta)
			assert.InDelta(t, 0.3+1.5+1.1, commSold.GetPeak(), delta)
			assert.InDelta(t, 2.0+2.0+2.0, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		case proto.CommodityDTO_VCPU_REQUEST:
			assert.InDelta(t, 1.0+0.89+0.69, commSold.GetUsed(), delta)
			assert.InDelta(t, 1.0+0.89+0.69, commSold.GetPeak(), delta)
			assert.InDelta(t, 1.9+1.9+1.9, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		case proto.CommodityDTO_VMEM:
			assert.InDelta(t, 3112000+1234000+4164400, commSold.GetUsed(), delta)
			assert.InDelta(t, 3112000+1234000+4164400, commSold.GetPeak(), delta)
			assert.InDelta(t, 8168868+8168868+8168868, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		case proto.CommodityDTO_VMEM_REQUEST:
			assert.InDelta(t, 140000+2552000+3600000, commSold.GetUsed(), delta)
			assert.InDelta(t, 140000+2552000+3600000, commSold.GetPeak(), delta)
			assert.InDelta(t, 8066468+8066468+8066468, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		case proto.CommodityDTO_VSTORAGE:
			assert.InDelta(t, 9385+23748+5678, commSold.GetUsed(), delta)
			assert.InDelta(t, 9385+23748+5678, commSold.GetPeak(), delta)
			assert.InDelta(t, 40470+40470+40470, commSold.GetCapacity(), delta)
			assert.False(t, commSold.GetResizable())
		default:
			assert.Fail(t, "Detected unsupported commodity sold %v", commSold)
		}
	}
	assert.False(t, clusterDTO.GetProviderPolicy().GetAvailableForPlacement())
}

func Test_clusterDTOBuilder_createClusterData(t *testing.T) {
	kubeCluster := repository.KubeCluster{Name: clusterId}
	clusterSummary := repository.ClusterSummary{KubeCluster: &kubeCluster}
	builder := NewClusterDTOBuilder(&clusterSummary, targetId)
	namespaceDTOs := []*proto.EntityDTO{namespaceDTO1, namespaceDTO2}
	nodeResourceCapacityMap := map[proto.CommodityDTO_CommodityType]float64{
		proto.CommodityDTO_VCPU: nodeVCpuCap,
		proto.CommodityDTO_VMEM: nodeVMemCap,
	}
	clusterData := builder.createClusterData(clusterId, namespaceDTOs, nodeResourceCapacityMap)

	expectedVcpuOvercommitment := (nsVcpuLimitQuotaUsage1 + nsVCpuLimitQuotaUsage2) / nodeVCpuCap
	expectedVmemOvercommitment := (nsVmemLimitQuotaUsage1 + nsVMemLimitQuotaUsage2) / nodeVMemCap
	assert.InDelta(t, expectedVcpuOvercommitment, *clusterData.VcpuOvercommitment, delta)
	assert.InDelta(t, expectedVmemOvercommitment, *clusterData.VmemOvercommitment, delta)
}

func createNSCommodityDTO(commodityType proto.CommodityDTO_CommodityType, used float64) *proto.CommodityDTO {
	return &proto.CommodityDTO{
		CommodityType: &commodityType,
		Used:          &used,
	}
}
