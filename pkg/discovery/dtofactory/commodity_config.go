package dtofactory

const (
	vcpuThrottlingUtilThresholdDefault = 30.0
)

type CommodityConfig struct {
	// VCPU Throttling threshold
	VCPUThrottlingUtilThreshold float64
}

func DefaultCommodityConfig() *CommodityConfig {
	return &CommodityConfig{
		VCPUThrottlingUtilThreshold: vcpuThrottlingUtilThresholdDefault,
	}
}
