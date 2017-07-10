package old

// TODO we will re-include provisioned commodities sold by node later.
type NodeResourceStat struct {
	vCpuCapacity float64
	vCpuUsed     float64
	vMemCapacity float64
	vMemUsed     float64
	//cpuProvisionedCapacity float64
	//cpuProvisionedUsed     float64
	//memProvisionedCapacity float64
	//memProvisionedUsed     float64
}

// TODO we will re-include provisioned commodities bought by pod later.
type PodResourceStat struct {
	vCpuCapacity float64
	vCpuReserved float64
	vCpuUsed     float64
	vMemCapacity float64
	vMemReserved float64
	vMemUsed     float64
	//cpuProvisionedCapacity float64
	//cpuProvisionedUsed     float64
	//memProvisionedCapacity float64
	//memProvisionedUsed     float64
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
