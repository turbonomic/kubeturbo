package configs


import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
)

type ProbeConfig struct {
	CadvisorPort int

	// A correct stitching property type is the prerequisite for stitching process.
	StitchingPropertyType stitching.StitchingPropertyType

	MonitoringConfigs []monitoring.MonitorWorkerConfig
}
