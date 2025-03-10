/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Most code are copied from the K8s adminssion plugin
Tweak a little bit to make it suitable for usage here
github.com/kubernetes/plugin/pkg/admission/limitranger/admission.go
*/
package executor

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	kclient "k8s.io/client-go/kubernetes"
)

func CheckLimitrangeViolationOnPod(client *kclient.Clientset, namespace string, pod *corev1.Pod) error {
	glog.V(4).Infof("Start to check LimitRange violation in the namespace %s based on the pod spec %+v", namespace, pod)
	if client == nil || pod == nil || namespace == "" {
		return fmt.Errorf("check failed due to a empty client/pod/namespace")
	}
	limitRanges, err := client.CoreV1().LimitRanges(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		glog.Errorf("can't list limitrange due to :%v", err)
		return err
	}
	for _, limitrange := range limitRanges.Items {
		// Enfource resource requirement against enumerated constraints on the LimitRange
		// It may modify pod to apply default resource requirements
		PodMutateLimitFunc(&limitrange, pod)
		glog.V(4).Infof("The desired podspec becomes %+v after applying the default resource requirements of the limitrange %+v", pod, limitrange)
		if err := PodValidateLimitFunc(&limitrange, pod); err != nil {
			return err
		}
	}
	return nil //no limitrange
}

// PodValidateLimitFunc enforces resource requirements enumerated by the pod against
// the specified LimitRange.
func PodValidateLimitFunc(limitRange *corev1.LimitRange, pod *corev1.Pod) error {
	var errs []error

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		limitType := limit.Type
		// enforce container limits
		if limitType == corev1.LimitTypeContainer {
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
			for j := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[j]
				for k, v := range limit.Min {
					if err := minConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.Max {
					if err := maxConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
				for k, v := range limit.MaxLimitRequestRatio {
					if err := limitRequestRatioConstraint(string(limitType), string(k), v, container.Resources.Requests, container.Resources.Limits); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}

		// enforce pod limits on init containers
		if limitType == corev1.LimitTypePod {
			containerRequests, containerLimits := []corev1.ResourceList{}, []corev1.ResourceList{}
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				containerRequests = append(containerRequests, container.Resources.Requests)
				containerLimits = append(containerLimits, container.Resources.Limits)
			}
			podRequests := sum(containerRequests)
			podLimits := sum(containerLimits)
			for j := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[j]
				// take max(sum_containers, any_init_container)
				for k, v := range container.Resources.Requests {
					if v2, ok := podRequests[k]; ok {
						if v.Cmp(v2) > 0 {
							podRequests[k] = v
						}
					} else {
						podRequests[k] = v
					}
				}
				for k, v := range container.Resources.Limits {
					if v2, ok := podLimits[k]; ok {
						if v.Cmp(v2) > 0 {
							podLimits[k] = v
						}
					} else {
						podLimits[k] = v
					}
				}
			}
			for k, v := range limit.Min {
				if err := minConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.Max {
				if err := maxConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
			for k, v := range limit.MaxLimitRequestRatio {
				if err := limitRequestRatioConstraint(string(limitType), string(k), v, podRequests, podLimits); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

// ===========================================================================
// NOTE: This method has deviated from the upstream kubernetes code.
// If we ever pull the latest changes from upstream, we may need to manually
// update this method.
// ===========================================================================
// minConstraint enforces the min constraint over the specified resource
func minConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if reqExists && observedReqValue < enforcedValue {
		return fmt.Errorf("minimum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	if limExists && (observedLimValue < enforcedValue) {
		return fmt.Errorf("minimum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	return nil
}

// ===========================================================================
// NOTE: This method has deviated from the upstream kubernetes code.
// If we ever pull the latest changes from upstream, we may need to manually
// update this method.
// ===========================================================================
// maxConstraint enforces the max constraint over the specified resource
func maxConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, enforcedValue := requestLimitEnforcedValues(req, lim, enforced)

	if limExists && observedLimValue > enforcedValue {
		return fmt.Errorf("maximum %s usage per %s is %s, but limit is %s", resourceName, limitType, enforced.String(), lim.String())
	}
	if reqExists && (observedReqValue > enforcedValue) {
		return fmt.Errorf("maximum %s usage per %s is %s, but request is %s", resourceName, limitType, enforced.String(), req.String())
	}
	return nil
}

// requestLimitEnforcedValues returns the specified values at a common precision to support comparability
func requestLimitEnforcedValues(requestQuantity, limitQuantity, enforcedQuantity resource.Quantity) (request, limit, enforced int64) {
	request = requestQuantity.Value()
	limit = limitQuantity.Value()
	enforced = enforcedQuantity.Value()
	// do a more precise comparison if possible (if the value won't overflow)
	if request <= resource.MaxMilliValue && limit <= resource.MaxMilliValue && enforced <= resource.MaxMilliValue {
		request = requestQuantity.MilliValue()
		limit = limitQuantity.MilliValue()
		enforced = enforcedQuantity.MilliValue()
	}
	return
}

// limitRequestRatioConstraint enforces the limit to request ratio over the specified resource
func limitRequestRatioConstraint(limitType string, resourceName string, enforced resource.Quantity, request corev1.ResourceList, limit corev1.ResourceList) error {
	req, reqExists := request[corev1.ResourceName(resourceName)]
	lim, limExists := limit[corev1.ResourceName(resourceName)]
	observedReqValue, observedLimValue, _ := requestLimitEnforcedValues(req, lim, enforced)

	if !reqExists || (observedReqValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no request is specified or request is 0", resourceName, limitType, enforced.String())
	}
	if !limExists || (observedLimValue == int64(0)) {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but no limit is specified or limit is 0", resourceName, limitType, enforced.String())
	}

	observedRatio := float64(observedLimValue) / float64(observedReqValue)
	displayObservedRatio := observedRatio
	maxLimitRequestRatio := float64(enforced.Value())
	if enforced.Value() <= resource.MaxMilliValue {
		observedRatio = observedRatio * 1000
		maxLimitRequestRatio = float64(enforced.MilliValue())
	}

	if observedRatio > maxLimitRequestRatio {
		return fmt.Errorf("%s max limit to request ratio per %s is %s, but provided ratio is %f", resourceName, limitType, enforced.String(), displayObservedRatio)
	}

	return nil
}

// sum takes the total of each named resource across all inputs
// if a key is not in each input, then the output resource list will omit the key
func sum(inputs []corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	keys := []corev1.ResourceName{}
	for i := range inputs {
		for k := range inputs[i] {
			keys = append(keys, k)
		}
	}
	for _, key := range keys {
		total, isSet := int64(0), true

		for i := range inputs {
			input := inputs[i]
			v, exists := input[key]
			if exists {
				if key == corev1.ResourceCPU {
					total = total + v.MilliValue()
				} else {
					total = total + v.Value()
				}
			} else {
				isSet = false
			}
		}

		if isSet {
			if key == corev1.ResourceCPU {
				result[key] = *(resource.NewMilliQuantity(total, resource.DecimalSI))
			} else {
				result[key] = *(resource.NewQuantity(total, resource.DecimalSI))
			}

		}
	}
	return result
}

// PodMutateLimitFunc sets resource requirements enumerated by the pod against
// the specified LimitRange.  The pod may be modified to apply default resource
// requirements if not specified, and enumerated on the LimitRange
func PodMutateLimitFunc(limitRange *corev1.LimitRange, pod *corev1.Pod) {
	defaultResources := defaultContainerResourceRequirements(limitRange)
	mergePodResourceRequirements(pod, &defaultResources)
}

// defaultContainerResourceRequirements returns the default requirements for a container
// the requirement.Limits are taken from the LimitRange defaults (if specified)
// the requirement.Requests are taken from the LimitRange default request (if specified)
func defaultContainerResourceRequirements(limitRange *corev1.LimitRange) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{}
	requirements.Requests = corev1.ResourceList{}
	requirements.Limits = corev1.ResourceList{}

	for i := range limitRange.Spec.Limits {
		limit := limitRange.Spec.Limits[i]
		if limit.Type == corev1.LimitTypeContainer {
			for k, v := range limit.DefaultRequest {
				requirements.Requests[corev1.ResourceName(k)] = v.DeepCopy()
			}
			for k, v := range limit.Default {
				requirements.Limits[corev1.ResourceName(k)] = v.DeepCopy()
			}
		}
	}
	return requirements
}

// mergePodResourceRequirements merges enumerated requirements with default requirements
// it annotates the pod with information about what requirements were modified
func mergePodResourceRequirements(pod *corev1.Pod, defaultRequirements *corev1.ResourceRequirements) {

	for i := range pod.Spec.Containers {
		mergeContainerResources(&pod.Spec.Containers[i], defaultRequirements)
	}

	for i := range pod.Spec.InitContainers {
		mergeContainerResources(&pod.Spec.InitContainers[i], defaultRequirements)
	}
}

// ===========================================================================
// NOTE: This method has deviated from the upstream kubernetes code.
// If we ever pull the latest changes from upstream, we may need to manually
// update this method.
// ===========================================================================
// mergeContainerResources handles defaulting all of the resources on a container.
func mergeContainerResources(container *corev1.Container, defaultRequirements *corev1.ResourceRequirements) {
	if container.Resources.Limits != nil && len(container.Resources.Limits) > 0 {
		for k, v := range defaultRequirements.Limits {
			_, found := container.Resources.Limits[k]
			if !found {
				container.Resources.Limits[k] = v.DeepCopy()
			}
		}
	}
	if container.Resources.Requests != nil && len(container.Resources.Requests) > 0 {
		for k, v := range defaultRequirements.Requests {
			_, found := container.Resources.Requests[k]
			if !found {
				container.Resources.Requests[k] = v.DeepCopy()
			}
		}
	}
}
