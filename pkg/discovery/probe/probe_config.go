package probe

type ProbeConfig struct {
	CadvisorPort int

	// A flag to indicate whether the underlying infrastructure is VMWare. If true, use UUID for stitching.
	// Otherwise use IP.
	UseVMWare bool
}
