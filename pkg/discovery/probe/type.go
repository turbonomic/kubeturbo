package probe

type NodeResourceStat struct {
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
	vCpuCapacity           float64
	vCpuUsed               float64
	vMemCapacity           float64
	vMemUsed               float64
	cpuProvisionedCapacity float64
	cpuProvisionedUsed     float64
	memProvisionedCapacity float64
	memProvisionedUsed     float64
}

type ApplicationResourceStat struct {
	vCpuUsed            float64
	vMemUsed            float64
	transactionCapacity float64
	transactionUsed     float64
}

type ServiceResourceStat struct {
	transactionBought float64
}
