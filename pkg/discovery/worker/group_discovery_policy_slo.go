package worker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.ibm.com/turbonomic/turbo-policy/api/v1alpha1"
)

const (
	defaultMinReplicas int32 = 1
	defaultMaxReplicas int32 = 10
	minRangeReplicas   int32 = 1
	maxRangeReplicas   int32 = 10000
)

func buildSLOHorizontalScalePolicy(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO, error) {
	// Resolve group members
	resolvedSvcIds, err := resolveSLOPolicyTargets(worker, policyBinding)
	if err != nil {
		return nil, err
	}
	// Create policy settings
	settingPolicy, err := createSLOPolicy(worker, policyBinding)
	if err != nil {
		return nil, err
	}
	// Create a dynamic group and applies settings to the group
	displayName := fmt.Sprintf("%v [%v]", policyBinding, worker.targetId)
	return group.
		StaticRegularGroup(policyBinding.GetUID()).
		WithDisplayName(displayName).
		OfType(proto.EntityDTO_SERVICE).
		WithEntities(resolvedSvcIds).
		WithSettingPolicy(settingPolicy).
		Build()
}

// resolveSLOPolicyTargets resolve services specified in the policy targets
func resolveSLOPolicyTargets(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) ([]string, error) {
	targets := policyBinding.GetTargets()
	if len(targets) == 0 {
		return nil, fmt.Errorf("policy target is empty")
	}
	var names []string
	for _, target := range targets {
		// All targets must be Service
		if target.Kind != Service {
			return nil, fmt.Errorf("target %v is not a service", target)
		}
		names = append(names, target.Name)
	}
	mergedRegExNames := strings.Join(names, "|")
	namespace := policyBinding.GetNamespace()
	var resolvedSvcIds []string
	for service := range worker.cluster.Services {
		if namespace != service.GetNamespace() {
			continue
		}
		if matched, err := regexp.MatchString(mergedRegExNames, service.GetName()); err == nil && matched {
			resolvedSvcIds = append(resolvedSvcIds, string(service.GetUID()))
		} else if err != nil {
			return nil, err
		}
	}
	if len(resolvedSvcIds) == 0 {
		return nil, fmt.Errorf("failed to resolve service specified the policy targets")
	}
	return resolvedSvcIds, nil
}

// createSLOPolicy converts a TurboPolicy into Turbonomic group and policy settings
// The conversion aborts when there is error validating the policy settings
func createSLOPolicy(
	worker *k8sEntityGroupDiscoveryWorker,
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO_SettingPolicy_, error) {
	spec := policyBinding.GetSLOHorizontalScaleSpec()
	if spec == nil {
		return nil, fmt.Errorf("the SLOHorizontalScaleSpec is nil")
	}
	if len(spec.Objectives) == 0 {
		return nil, fmt.Errorf("no objective is set for SLO policy")
	}
	settings := group.NewSettingsBuilder()
	minReplicas, maxReplicas, err := validateReplicas(spec.MinReplicas, spec.MaxReplicas)
	if err != nil {
		return nil, err
	}
	if minReplicas != nil {
		settings.AddSetting(group.NewMinReplicasPolicySetting(float32(*minReplicas)))
	}
	if maxReplicas != nil {
		settings.AddSetting(group.NewMaxReplicasPolicySetting(float32(*maxReplicas)))
	}
	var settingValue float32
	for _, setting := range spec.Objectives {
		if err := json.Unmarshal(setting.Value.Raw, &settingValue); err != nil {
			return nil, err
		}
		if settingValue <= 0 {
			return nil, fmt.Errorf("setting %v has a non-positive value %v",
				setting.Name, settingValue)
		}
		switch setting.Name {
		case v1alpha1.ResponseTime:
			settings.AddSetting(group.NewResponseTimeSLOPolicySetting(settingValue))
		case v1alpha1.Transaction:
			settings.AddSetting(group.NewTransactionSLOPolicySetting(settingValue))
		default:
			return nil, fmt.Errorf("unknown objective name %v", setting.Name)
		}
	}
	behavior := spec.Behavior
	if behavior.HorizontalScaleDown != nil {
		mode, err := validateActionMode(*behavior.HorizontalScaleDown)
		if err != nil {
			return nil, err
		}
		settings.AddSetting(group.NewHorizontalScaleDownAutomationPolicySetting(mode))
	}
	if behavior.HorizontalScaleUp != nil {
		mode, err := validateActionMode(*behavior.HorizontalScaleUp)
		if err != nil {
			return nil, err
		}
		settings.AddSetting(group.NewHorizontalScaleUpAutomationPolicySetting(mode))
	}
	displayName := fmt.Sprintf("TurboPolicy::%v on %v [%v]",
		policyBinding.GetPolicyType(), policyBinding, worker.targetId)
	return group.NewSettingPolicyBuilder().
		WithDisplayName(displayName).
		WithName(policyBinding.GetUID()).
		WithSettings(settings.Build()).
		Build()
}

// validateReplicas validates minReplicas and maxReplicas
func validateReplicas(minReplicas *int32, maxReplicas *int32) (*int32, *int32, error) {
	if minReplicas == nil && maxReplicas == nil {
		return nil, nil, nil
	}
	if minReplicas != nil && !isWithinValidRange(*minReplicas) {
		return nil, nil, fmt.Errorf("invalid minReplicas %v. Must be between %v and %v inclusive", *minReplicas, minRangeReplicas, maxRangeReplicas)
	}
	if maxReplicas != nil && !isWithinValidRange(*maxReplicas) {
		return nil, nil, fmt.Errorf("invalid maxReplicas %v. Must be between %v and %v inclusive", *maxReplicas, minRangeReplicas, maxRangeReplicas)
	}
	if minReplicas != nil && maxReplicas != nil && *minReplicas > *maxReplicas {
		glog.Warningf("invalid SLOHorizontalScale: minReplicas (%v) must be less than or equal to maxReplicas (%v) -- using default min (%v) and max (%v) values",
			*minReplicas, *maxReplicas, defaultMinReplicas, defaultMaxReplicas)
		// Can't get the address of a constant so need to store it into a variable first
		min := defaultMinReplicas
		max := defaultMaxReplicas
		minReplicas = &min
		maxReplicas = &max
	}
	return minReplicas, maxReplicas, nil
}

// isWithinValidRange check if the number of replicas is within the valid range
func isWithinValidRange(replicas int32) bool {
	return replicas >= minRangeReplicas && replicas <= maxRangeReplicas
}
