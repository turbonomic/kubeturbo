package reservation

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"

	"github.com/vmturbo/kubeturbo/pkg/probe"

	"github.com/golang/glog"
)

var (
	templateTiny DeployTemplate = DeployTemplate{
		UUID:    "DC5_1CxZMJkEEeCaJOYu5",
		CpuSize: 2.0,
		MemSize: 8192,
	}
	templateMicro DeployTemplate = DeployTemplate{
		UUID:    "DC5_1CxgeJkEEeCaJOYu5",
		CpuSize: 2.0,
		MemSize: 4096,
	}
	templateSmall DeployTemplate = DeployTemplate{
		UUID:    "DC5_1CxZMJkbfjCaJOYu5",
		CpuSize: 1.0,
		MemSize: 2048,
	}
	templateMedium DeployTemplate = DeployTemplate{
		UUID:    "DC5_1CxZMJfgejCaJOYu5",
		CpuSize: 1.0,
		MemSize: 1024,
	}
	templateLarge DeployTemplate = DeployTemplate{
		UUID:    "DC5_1CxZMJkghjCaJOYu5",
		CpuSize: 0.5,
		MemSize: 512,
	}

	// Order is strict!
	availableTemplates []DeployTemplate = []DeployTemplate{templateTiny, templateMicro, templateSmall, templateMedium, templateLarge}
)

type DeployTemplate struct {
	UUID    string
	CpuSize float64
	MemSize float64
}

func SelectTemplate(cpuLimit float64, memLimit float64) (string, error) {
	glog.V(4).Infof("Try to find template to match %f cpu and %f mem", cpuLimit, memLimit)
	for _, t := range availableTemplates {
		if t.CpuSize > cpuLimit && t.MemSize > memLimit {
			return t.UUID, nil
		}
	}
	// TODO, if case of there is no tempalte with enough resource, should we return an error.
	return "", fmt.Errorf("Cannot find deploy template to match required resource limits.")
}

func SelectTemplateForPod(pod *api.Pod) (string, error) {
	cpuLimit, memLimit, err := probe.GetResourceLimits(pod)

	if err != nil {
		return "", err
	}
	if cpuLimit == 0 && memLimit == 0 {
		return templateLarge.UUID, nil
	}
	// VMTurbo uses Mb
	return SelectTemplate(cpuLimit, memLimit/1024)
}
