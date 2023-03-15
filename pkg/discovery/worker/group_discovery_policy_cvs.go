package worker

import (
	"fmt"
	"regexp"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func buildContainerVerticalScalePolicy(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO, error) {
	// Resolve group members

	resolvedIds, err := resolveCVSPolicyTargets(worker, policyBinding)
	if err != nil {
		return nil, err
	}
	// Create policy settings
	settingPolicy, err := createCVSPolicy(worker, policyBinding)
	if err != nil {
		return nil, err
	}
	// Create a dynamic group and applies settings to the group
	displayName := fmt.Sprintf("%v [%v]", policyBinding, worker.targetId)
	return group.
		StaticRegularGroup(policyBinding.GetUID()).
		WithDisplayName(displayName).
		OfType(proto.EntityDTO_CONTAINER_SPEC).
		WithEntities(resolvedIds).
		WithSettingPolicy(settingPolicy).
		Build()
}

// resolveCVSPolicyTargets resolve containers specified in the targets of contianer vertical scale policy.
func resolveCVSPolicyTargets(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) ([]string, error) {
	targets := policyBinding.GetTargets()
	if len(targets) == 0 {
		return nil, fmt.Errorf("policy target is empty")
	}

	var resolvedIds []string
	for _, target := range targets {
		// All targets must be container in a workload controller
		if !validWorkloadController(target.Kind) {
			return nil, fmt.Errorf("target %v is not a workload controller", target)
		}

		controllerRegex := target.Name
		containerRegex := target.Container
		namespace := policyBinding.GetNamespace()

		for _, value := range worker.cluster.ControllerMap {
			if namespace != value.Namespace || !validWorkloadController(value.Kind) {
				continue
			}
			if matched, err := regexp.MatchString(controllerRegex, value.Name); err == nil && matched {
				for container := range value.Containers {
					if matched, err := regexp.MatchString(containerRegex, container); err == nil && matched {
						containerSpecId := util.ContainerSpecIdFunc(value.UID, container)
						resolvedIds = append(resolvedIds, containerSpecId)
					} else if err != nil {
						return nil, err
					}
				}
			} else if err != nil {
				return nil, err
			}
		}
	}

	if len(resolvedIds) == 0 {
		return nil, fmt.Errorf("failed to resolve container specified the policy targets")
	}

	return resolvedIds, nil

}

// createCVSPolicy converts a TurboPolicy into Turbonomic group and policy settings
// The conversion aborts when there is error validating the policy settings
func createCVSPolicy(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO_SettingPolicy_, error) {
	spec := policyBinding.GetContainerVerticalScaleSpec()
	if spec == nil {
		return nil, fmt.Errorf("the ContainerVerticalScaleSpec is nil")
	}

	settings := group.NewSettingsBuilder()

	aggressiveness := spec.Settings.Aggressiveness
	if aggressiveness != nil {
		val := PercentileAggressiveness[(string(*aggressiveness))]
		glog.V(2).Infof("Aggressiveness %f", val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_AGGRESSIVENESS, val))
	}

	rateOfResize := spec.Settings.RateOfResize
	if rateOfResize != nil {
		val, _ := RateOfResize[(string(*rateOfResize))]
		glog.V(2).Infof("RateOfResize %f", val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_RATE_OF_RESIZE, val))
	}

	cpuThrottlingTolerance := spec.Settings.CpuThrottlingTolerance
	if cpuThrottlingTolerance != nil {
		val, err := parseFloatFromPercentString(string(*cpuThrottlingTolerance))
		if err != nil {
			return nil, err
		}
		glog.V(2).Infof("CpuThrottlingTolerance %f", val)
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_VCPU_MAX_THROTTLING_TOLERANCE, val))
	}

	increments := spec.Settings.Increments
	if increments != nil {
		cpu := increments.CPU
		if cpu != nil {
			val, err := QuantityToMilliCore(cpu)
			if err != nil {
				return nil, err
			}
			glog.V(2).Infof("Increments.CPU %f", val)
			settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_INCREMENT_RESIZE_CONSTANT_VCPU, val))
		}

		mem := increments.Memory
		if mem != nil {
			val, err := QuantityToMB(mem)
			if err != nil {
				return nil, err
			}
			glog.V(2).Infof("Increments.Mem %f", val)
			settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_INCREMENT_RESIZE_CONSTANT_VMEM, val))
		}
	}

	observationPeriod := spec.Settings.ObservationPeriod
	if observationPeriod != nil {
		max := observationPeriod.Max
		if max != nil {
			val := MaxObservationPeriod[string(*max)]
			glog.V(2).Infof("ObservationPeriod.Max %f", val)
			settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_MAX_OBSERVATION_PERIOD, val))
		}

		min := observationPeriod.Min
		if min != nil {
			val := MinObservationPeriod[string(*min)]
			glog.V(2).Infof("ObservationPeriod.Min %f", val)
			settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_MIN_OBSERVATION_PERIOD, val))
		}
	}

	limits := spec.Settings.Limits
	if limits != nil {
		err := addCVSLimitSettings("VCPU_LIMIT_RESIZE", limits.CPU, settings)
		if err != nil {
			return nil, err
		}
		err = addCVSLimitSettings("VMEM_LIMIT_RESIZE", limits.Memory, settings)
		if err != nil {
			return nil, err
		}
	}

	requests := spec.Settings.Requests
	if requests != nil {
		err := addCVSRequestSettings("VCPU_REQUEST_RESIZE", requests.CPU, settings)
		if err != nil {
			return nil, err
		}
		err = addCVSRequestSettings("VMEM_REQUEST_RESIZE", requests.Memory, settings)
		if err != nil {
			return nil, err
		}
	}

	// need to implement adding setting with discovered spec
	//settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_RATE_OF_RESIZE, float32(22.0)))
	behavior := spec.Behavior
	if behavior.VerticalResize != nil {
		mode, err := validateActionMode(*behavior.VerticalResize)
		if err != nil {
			return nil, err
		}
		settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_RESIZE_AUTOMATION_MODE, mode))
	}
	sts := settings.Build()

	displayName := fmt.Sprintf("TurboPolicy::%v on %v [%v]",
		policyBinding.GetPolicyType(), policyBinding, worker.targetId)
	return group.NewSettingPolicyBuilder().
		WithDisplayName(displayName).
		WithName(policyBinding.GetUID()).
		WithSettings(sts).
		Build()
}
