package probe

type NodeResourceStat struct {
	cpuAllocationCapacity  float64
	cpuAllocationUsed      float64
	memAllocationCapacity  float64
	memAllocationUsed      float64
	vCpuCapacity           float64
	vCpuUsed               float64
	vMemCapacity           float64
	vMemUsed               float64
	cpuProvisionedCapacity float64
	cpuProvisionedUsed     float64
	memProvisionedCapacity float64
	memProvisionedUsed     float64
}

type PodResourceStat struct {
	cpuAllocationCapacity  float64
	cpuAllocationUsed      float64
	memAllocationCapacity  float64
	memAllocationUsed      float64
	cpuProvisionedCapacity float64
	cpuProvisionedUsed     float64
	memProvisionedCapacity float64
	memProvisionedUsed     float64
}

type ApplicationResourceStat struct {
	cpuAllocationUsed   float64
	memAllocationUsed   float64
	vCpuUsed            float64
	vMemUsed            float64
	transactionCapacity float64
	transactionUsed     float64
}

type ServiceResourceStat struct {
	transactionBought float64
}
