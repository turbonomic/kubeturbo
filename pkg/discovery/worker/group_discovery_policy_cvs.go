package worker

import (
	"fmt"
	"regexp"

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
		OfType(proto.EntityDTO_CONTAINER).
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

		for key, value := range worker.cluster.ControllerMap {
			fmt.Println(key, value)
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
	// need to implement adding setting with discovered spec
	settings.AddSetting(group.NewPolicySetting(proto.GroupDTO_Setting_RATE_OF_RESIZE, float32(22.0)))
	sts := settings.Build()

	displayName := fmt.Sprintf("TurboPolicy::%v on %v [%v]",
		policyBinding.GetPolicyType(), policyBinding, worker.targetId)
	return group.NewSettingPolicyBuilder().
		WithDisplayName(displayName).
		WithName(policyBinding.GetUID()).
		WithSettings(sts).
		Build()
}
