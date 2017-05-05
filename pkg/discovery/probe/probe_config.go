package probe

import "github.com/turbonomic/kubeturbo/pkg/discovery/probe/stitching"

type ProbeConfig struct {
	CadvisorPort int

	// A correct stitching property type is the prerequisite for stitching process.
	StitchingPropertyType stitching.StitchingPropertyType
}
