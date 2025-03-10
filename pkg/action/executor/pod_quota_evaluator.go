/*
Copied from k8s.io/kubernetes/pkg/quota/v1/evaluator/core/pods.go
*/

package executor

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/utils/clock"
)

// the name used for object count quota
var podObjectCountName = generic.ObjectCountQuotaResourceNameFor(corev1.SchemeGroupVersion.WithResource("pods").GroupResource())

// podResources are the set of resources managed by quota associated with pods.
var podResources = []corev1.ResourceName{
	podObjectCountName,
	corev1.ResourceCPU,
	corev1.ResourceMemory,
	corev1.ResourceEphemeralStorage,
	corev1.ResourceRequestsCPU,
	corev1.ResourceRequestsMemory,
	corev1.ResourceRequestsEphemeralStorage,
	corev1.ResourceLimitsCPU,
	corev1.ResourceLimitsMemory,
	corev1.ResourceLimitsEphemeralStorage,
	corev1.ResourcePods,
}

// podResourcePrefixes are the set of prefixes for resources (Hugepages, and other
// potential extended resources with specific prefix) managed by quota associated with pods.
var podResourcePrefixes = []string{
	corev1.ResourceHugePagesPrefix,
	corev1.ResourceRequestsHugePagesPrefix,
}

// requestedResourcePrefixes are the set of prefixes for resources
// that might be declared in pod's Resources.Requests/Limits
var requestedResourcePrefixes = []string{
	corev1.ResourceHugePagesPrefix,
}

// maskResourceWithPrefix mask resource with certain prefix
// e.g. hugepages-XXX -> requests.hugepages-XXX
func maskResourceWithPrefix(resource corev1.ResourceName, prefix string) corev1.ResourceName {
	return corev1.ResourceName(fmt.Sprintf("%s%s", prefix, string(resource)))
}

// isExtendedResourceNameForQuota returns true if the extended resource name
// has the quota related resource prefix.
func isExtendedResourceNameForQuota(name corev1.ResourceName) bool {
	// As overcommit is not supported by extended resources for now,
	// only quota objects in format of "requests.resourceName" is allowed.
	return !IsNativeResource(name) && strings.HasPrefix(string(name), corev1.DefaultResourceRequestsPrefix)
}

// NewPodEvaluator returns an evaluator that can evaluate pods
func NewPodEvaluator(f quota.ListerForResourceFunc, clock clock.Clock) *podEvaluator {
	listFuncByNamespace := generic.ListResourceUsingListerFunc(f, corev1.SchemeGroupVersion.WithResource("pods"))
	podEvaluator := &podEvaluator{listFuncByNamespace: listFuncByNamespace, clock: clock}
	return podEvaluator
}

// podEvaluator knows how to measure usage of pods.
type podEvaluator struct {
	// knows how to list pods
	listFuncByNamespace generic.ListFuncByNamespace
	// used to track time
	clock clock.Clock
}

// Usage knows how to measure usage associated with pods
func (p *podEvaluator) Usage(item runtime.Object) (corev1.ResourceList, error) {
	// delegate to normal usage
	return PodUsageFunc(item, p.clock)
}

// Matches returns true if the evaluator matches the specified quota with the provided input item
func (p *podEvaluator) Matches(resourceQuota *corev1.ResourceQuota, item runtime.Object) (bool, error) {
	return generic.Matches(resourceQuota, item, p.MatchingResources, podMatchesScopeFunc)
}

// MatchingResources takes the input specified list of resources and returns the set of resources it matches.
func (p *podEvaluator) MatchingResources(input []corev1.ResourceName) []corev1.ResourceName {
	result := quota.Intersection(input, podResources)
	for _, resource := range input {
		// for resources with certain prefix, e.g. hugepages
		if quota.ContainsPrefix(podResourcePrefixes, resource) {
			result = append(result, resource)
		}
		// for extended resources
		if isExtendedResourceNameForQuota(resource) {
			result = append(result, resource)
		}
	}

	return result
}

// GroupResource that this evaluator tracks
func (p *podEvaluator) GroupResource() schema.GroupResource {
	return corev1.SchemeGroupVersion.WithResource("pods").GroupResource()
}

// podComputeUsageHelper can summarize the pod compute quota usage based on requests and limits
func podComputeUsageHelper(requests corev1.ResourceList, limits corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	result[corev1.ResourcePods] = resource.MustParse("1")
	if request, found := requests[corev1.ResourceCPU]; found {
		result[corev1.ResourceCPU] = request
		result[corev1.ResourceRequestsCPU] = request
	}
	if limit, found := limits[corev1.ResourceCPU]; found {
		result[corev1.ResourceLimitsCPU] = limit
	}
	if request, found := requests[corev1.ResourceMemory]; found {
		result[corev1.ResourceMemory] = request
		result[corev1.ResourceRequestsMemory] = request
	}
	if limit, found := limits[corev1.ResourceMemory]; found {
		result[corev1.ResourceLimitsMemory] = limit
	}
	if request, found := requests[corev1.ResourceEphemeralStorage]; found {
		result[corev1.ResourceEphemeralStorage] = request
		result[corev1.ResourceRequestsEphemeralStorage] = request
	}
	if limit, found := limits[corev1.ResourceEphemeralStorage]; found {
		result[corev1.ResourceLimitsEphemeralStorage] = limit
	}
	for resource, request := range requests {
		// for resources with certain prefix, e.g. hugepages
		if quota.ContainsPrefix(requestedResourcePrefixes, resource) {
			result[resource] = request
			result[maskResourceWithPrefix(resource, corev1.DefaultResourceRequestsPrefix)] = request
		}
		// for extended resources
		if IsExtendedResourceName(resource) {
			// only quota objects in format of "requests.resourceName" is allowed for extended resource.
			result[maskResourceWithPrefix(resource, corev1.DefaultResourceRequestsPrefix)] = request
		}
	}

	return result
}

func toExternalPodOrError(obj runtime.Object) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	switch t := obj.(type) {
	case *corev1.Pod:
		pod = t
	default:
		return nil, fmt.Errorf("expect *api.Pod or *v1.Pod, got %v", t)
	}
	return pod, nil
}

// podMatchesScopeFunc is a function that knows how to evaluate if a pod matches a scope
func podMatchesScopeFunc(selector corev1.ScopedResourceSelectorRequirement, object runtime.Object) (bool, error) {
	pod, err := toExternalPodOrError(object)
	if err != nil {
		return false, err
	}
	switch selector.ScopeName {
	case corev1.ResourceQuotaScopeTerminating:
		return isTerminating(pod), nil
	case corev1.ResourceQuotaScopeNotTerminating:
		return !isTerminating(pod), nil
	case corev1.ResourceQuotaScopeBestEffort:
		return isBestEffort(pod), nil
	case corev1.ResourceQuotaScopeNotBestEffort:
		return !isBestEffort(pod), nil
	case corev1.ResourceQuotaScopePriorityClass:
		return podMatchesSelector(pod, selector)
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return usesCrossNamespacePodAffinity(pod), nil
	}
	return false, nil
}

// PodUsageFunc returns the quota usage for a pod.
// A pod is charged for quota if the following are not true.
//   - pod has a terminal phase (failed or succeeded)
//   - pod has been marked for deletion and grace period has expired
func PodUsageFunc(obj runtime.Object, clock clock.Clock) (corev1.ResourceList, error) {
	pod, err := toExternalPodOrError(obj)
	if err != nil {
		return corev1.ResourceList{}, err
	}

	// always quota the object count (even if the pod is end of life)
	// object count quotas track all objects that are in storage.
	// where "pods" tracks all pods that have not reached a terminal state,
	// count/pods tracks all pods independent of state.
	result := corev1.ResourceList{
		podObjectCountName: *(resource.NewQuantity(1, resource.DecimalSI)),
	}

	// by convention, we do not quota compute resources that have reached end-of life
	// note: the "pods" resource is considered a compute resource since it is tied to life-cycle.
	if !QuotaV1Pod(pod, clock) {
		return result, nil
	}

	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	// TODO: ideally, we have pod level requests and limits in the future.
	for i := range pod.Spec.Containers {
		requests = quota.Add(requests, pod.Spec.Containers[i].Resources.Requests)
		limits = quota.Add(limits, pod.Spec.Containers[i].Resources.Limits)
	}
	// InitContainers are run sequentially before other containers start, so the highest
	// init container resource is compared against the sum of app containers to determine
	// the effective usage for both requests and limits.
	for i := range pod.Spec.InitContainers {
		requests = quota.Max(requests, pod.Spec.InitContainers[i].Resources.Requests)
		limits = quota.Max(limits, pod.Spec.InitContainers[i].Resources.Limits)
	}

	requests = quota.Add(requests, pod.Spec.Overhead)
	limits = quota.Add(limits, pod.Spec.Overhead)

	result = quota.Add(result, podComputeUsageHelper(requests, limits))
	return result, nil
}

func isBestEffort(pod *corev1.Pod) bool {
	return GetPodQOS(pod) == corev1.PodQOSBestEffort
}

func isTerminating(pod *corev1.Pod) bool {
	if pod.Spec.ActiveDeadlineSeconds != nil && *pod.Spec.ActiveDeadlineSeconds >= int64(0) {
		return true
	}
	return false
}

func podMatchesSelector(pod *corev1.Pod, selector corev1.ScopedResourceSelectorRequirement) (bool, error) {
	labelSelector, err := ScopedResourceSelectorRequirementsAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("failed to parse and convert selector: %v", err)
	}
	var m map[string]string
	if len(pod.Spec.PriorityClassName) != 0 {
		m = map[string]string{string(corev1.ResourceQuotaScopePriorityClass): pod.Spec.PriorityClassName}
	}
	if labelSelector.Matches(labels.Set(m)) {
		return true, nil
	}
	return false, nil
}

func crossNamespacePodAffinityTerm(term *corev1.PodAffinityTerm) bool {
	return len(term.Namespaces) != 0 || term.NamespaceSelector != nil
}

func crossNamespacePodAffinityTerms(terms []corev1.PodAffinityTerm) bool {
	for _, t := range terms {
		if crossNamespacePodAffinityTerm(&t) {
			return true
		}
	}
	return false
}

func crossNamespaceWeightedPodAffinityTerms(terms []corev1.WeightedPodAffinityTerm) bool {
	for _, t := range terms {
		if crossNamespacePodAffinityTerm(&t.PodAffinityTerm) {
			return true
		}
	}
	return false
}

func usesCrossNamespacePodAffinity(pod *corev1.Pod) bool {
	if pod == nil || pod.Spec.Affinity == nil {
		return false
	}

	affinity := pod.Spec.Affinity.PodAffinity
	if affinity != nil {
		if crossNamespacePodAffinityTerms(affinity.RequiredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
		if crossNamespaceWeightedPodAffinityTerms(affinity.PreferredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
	}

	antiAffinity := pod.Spec.Affinity.PodAntiAffinity
	if antiAffinity != nil {
		if crossNamespacePodAffinityTerms(antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
		if crossNamespaceWeightedPodAffinityTerms(antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
	}

	return false
}

// QuotaV1Pod returns true if the pod is eligible to track against a quota
// if it's not in a terminal state according to its phase.
func QuotaV1Pod(pod *corev1.Pod, clock clock.Clock) bool {
	// if pod is terminal, ignore it for quota
	if corev1.PodFailed == pod.Status.Phase || corev1.PodSucceeded == pod.Status.Phase {
		return false
	}
	// if pods are stuck terminating (for example, a node is lost), we do not want
	// to charge the user for that pod in quota because it could prevent them from
	// scaling up new pods to service their application.
	if pod.DeletionTimestamp != nil && pod.DeletionGracePeriodSeconds != nil {
		now := clock.Now()
		deletionTime := pod.DeletionTimestamp.Time
		gracePeriod := time.Duration(*pod.DeletionGracePeriodSeconds) * time.Second
		if now.After(deletionTime.Add(gracePeriod)) {
			return false
		}
	}
	return true
}

/*
Copied from k8s.io/kubernetes/pkg/apis/core/v1/helper/qos/qos.go
*/

// GetPodQOS returns the QoS class of a pod.
// A pod is besteffort if none of its containers have specified any requests or limits.
// A pod is guaranteed only when requests and limits are specified for all the containers and they are equal.
// A pod is burstable if limits and requests do not match across all containers.
func GetPodQOS(pod *v1.Pod) v1.PodQOSClass {
	requests := v1.ResourceList{}
	limits := v1.ResourceList{}
	zeroQuantity := resource.MustParse("0")
	isGuaranteed := true
	allContainers := []v1.Container{}
	allContainers = append(allContainers, pod.Spec.Containers...)
	allContainers = append(allContainers, pod.Spec.InitContainers...)
	for _, container := range allContainers {
		// process requests
		for name, quantity := range container.Resources.Requests {
			if !isSupportedQoSComputeResource(name) {
				continue
			}
			if quantity.Cmp(zeroQuantity) == 1 {
				delta := quantity.DeepCopy()
				if _, exists := requests[name]; !exists {
					requests[name] = delta
				} else {
					delta.Add(requests[name])
					requests[name] = delta
				}
			}
		}
		// process limits
		qosLimitsFound := sets.NewString()
		for name, quantity := range container.Resources.Limits {
			if !isSupportedQoSComputeResource(name) {
				continue
			}
			if quantity.Cmp(zeroQuantity) == 1 {
				qosLimitsFound.Insert(string(name))
				delta := quantity.DeepCopy()
				if _, exists := limits[name]; !exists {
					limits[name] = delta
				} else {
					delta.Add(limits[name])
					limits[name] = delta
				}
			}
		}

		if !qosLimitsFound.HasAll(string(v1.ResourceMemory), string(v1.ResourceCPU)) {
			isGuaranteed = false
		}
	}
	if len(requests) == 0 && len(limits) == 0 {
		return v1.PodQOSBestEffort
	}
	// Check is requests match limits for all resources.
	if isGuaranteed {
		for name, req := range requests {
			if lim, exists := limits[name]; !exists || lim.Cmp(req) != 0 {
				isGuaranteed = false
				break
			}
		}
	}
	if isGuaranteed &&
		len(requests) == len(limits) {
		return v1.PodQOSGuaranteed
	}
	return v1.PodQOSBurstable
}

var supportedQoSComputeResources = sets.NewString("cpu", "memory")

func isSupportedQoSComputeResource(name v1.ResourceName) bool {
	return supportedQoSComputeResources.Has(string(name))
}

/*
Copied from k8s.io/kubernetes/pkg/apis/core/v1/helper/helpers.go
*/

// IsNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace. Partially-qualified (unprefixed) names are
// implicitly in the kubernetes.io/ namespace.
func IsNativeResource(name v1.ResourceName) bool {
	return !strings.Contains(string(name), "/") ||
		IsPrefixedNativeResource(name)
}

// IsPrefixedNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace.
func IsPrefixedNativeResource(name v1.ResourceName) bool {
	return strings.Contains(string(name), v1.ResourceDefaultNamespacePrefix)
}

// ScopedResourceSelectorRequirementsAsSelector converts the ScopedResourceSelectorRequirement api type into a struct that implements
// labels.Selector.
func ScopedResourceSelectorRequirementsAsSelector(ssr v1.ScopedResourceSelectorRequirement) (labels.Selector, error) {
	selector := labels.NewSelector()
	var op selection.Operator
	switch ssr.Operator {
	case v1.ScopeSelectorOpIn:
		op = selection.In
	case v1.ScopeSelectorOpNotIn:
		op = selection.NotIn
	case v1.ScopeSelectorOpExists:
		op = selection.Exists
	case v1.ScopeSelectorOpDoesNotExist:
		op = selection.DoesNotExist
	default:
		return nil, fmt.Errorf("%q is not a valid scope selector operator", ssr.Operator)
	}
	r, err := labels.NewRequirement(string(ssr.ScopeName), op, ssr.Values)
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*r)
	return selector, nil
}

// IsExtendedResourceName returns true if:
// 1. the resource name is not in the default namespace;
// 2. resource name does not have "requests." prefix,
// to avoid confusion with the convention in quota
// 3. it satisfies the rules in IsQualifiedName() after converted into quota resource name
func IsExtendedResourceName(name v1.ResourceName) bool {
	if IsNativeResource(name) || strings.HasPrefix(string(name), v1.DefaultResourceRequestsPrefix) {
		return false
	}
	// Ensure it satisfies the rules in IsQualifiedName() after converted into quota resource name
	nameForQuota := fmt.Sprintf("%s%s", v1.DefaultResourceRequestsPrefix, string(name))
	if errs := validation.IsQualifiedName(string(nameForQuota)); len(errs) != 0 {
		return false
	}
	return true
}
