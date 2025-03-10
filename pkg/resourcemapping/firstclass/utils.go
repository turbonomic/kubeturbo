package firstclass

import (
	"context"
	"fmt"
	"strings"

	"github.ibm.com/turbonomic/orm/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	errorMessageMissingResource = "missing resources"
	errorMessageNotEnoughQuota  = "not enough quota for resource"

	fieldSelectorPatternForRunningPodsOnNode = "status.phase=Running,spec.nodeName="

	quotaGVR = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "resourcequotas",
	}

	prefixRequests = "requests."
	prefixLimits   = "limits."
)

// only check requests for now
func checkNamespaceQuotaForResources(client dynamic.Interface, namespace string, resources corev1.ResourceList, replicas int64) error {
	if client == nil {
		return fmt.Errorf("client is nil for quota checking")
	}

	objList, err := client.Resource(quotaGVR).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(objList.Items) == 0 {
		return nil
	}

	// consolidate all quota objects in namespace
	quotas := make(corev1.ResourceList)
	qStatus := make(corev1.ResourceList)
	for _, obj := range objList.Items {
		qObj := &corev1.ResourceQuota{}
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, qObj); err != nil {
			return err
		}
		for r, q := range qObj.Status.Hard {
			quantity, exists := quotas[r]
			// handle k8s quota special case: resource does not exists or recorded quota is than the quota defined in this quota object, use new quantity
			if !exists || quantity.Cmp(q) > 0 {
				quotas[r] = q
			}
		}
		for r, q := range qObj.Status.Used {
			// avoid to double count the status.used in multiple resourcequota object
			if _, exists := qStatus[r]; exists {
				continue
			}
			// k8s ensures quota status.used has counter part in status.hard
			quantity := quotas[r]
			quantity.Sub(q)
			qStatus[r] = quantity
		}
	}

	// check quota requirement of the object
	for r, q := range resources {
		quantity, exists := qStatus[corev1.ResourceName(prefixRequests)+r]
		if !exists {
			continue
		}
		q.Mul(replicas)
		if quantity.Cmp(q) < 0 {
			q.Sub(quantity)
			return fmt.Errorf("%s %s, missing: %s", errorMessageNotEnoughQuota, r, q.String())
		}
	}

	return nil
}

func prettyResourceListOutput(res corev1.ResourceList) string {
	var result strings.Builder

	for r, q := range res {
		result.WriteString(r.String() + ":" + q.String() + "")
	}

	return result.String()
}

var (
	podGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}

	nodeGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "nodes",
	}
)

// In order to work with autoscalers like KPA, turbonomic sets the min/max replica number in owner resource (e.g. ksvc) to the same value.
// Assets like knative generate new version every time there is a change to its spec including the min/max replicas change above.
// In this rolling update situation, Pods in legacy deployment won't be removed until all Pods in new deployment are running.
// The additional resource requirements during this rolling update and could put entire process on hold (new Pods in Pending status)
// Kubeturbo should check the resource availability before take the action.
// If there is not enough resource for rolling update, it does not start the update and returns an error
func checkResourceForRolling(client dynamic.Interface, selectors []metav1.LabelSelector, resources corev1.ResourceList, replicas int) error {

	if resources == nil || len(resources) == 0 {
		return nil
	}

	if len(selectors) == 0 {
		selectors = []metav1.LabelSelector{{}}
	}
	finished := make(map[string]bool)

	for _, selector := range selectors {

		// filter nodes
		ls, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return err
		}
		nodeList, err := client.Resource(nodeGVR).List(context.TODO(), metav1.ListOptions{LabelSelector: ls.String()})
		if err != nil {
			return err
		}

		// check the resource node by node to avoid to avoid heavy communication with tons of objects all together
		for _, nodeObj := range nodeList.Items {
			// overlapped nodes from filters
			if _, exists := finished[nodeObj.GetName()]; exists {
				continue
			}
			finished[nodeObj.GetName()] = true

			// get all resources of the node
			node := corev1.Node{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(nodeObj.Object, &node)
			if err != nil {
				return err
			}

			resList := make(corev1.ResourceList)
			for res, q := range node.Status.Allocatable {
				resList[res] = q
			}

			// minus used resources in running pods
			podList, err := client.Resource(podGVR).List(context.TODO(), metav1.ListOptions{FieldSelector: fieldSelectorPatternForRunningPodsOnNode + node.Name})
			if err != nil {
				return err
			}

			for _, podObj := range podList.Items {
				pod := corev1.Pod{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(podObj.Object, &pod)
				if err != nil {
					return err
				}

				for _, c := range pod.Spec.Containers {
					l := getConvergedResourceListFromContainer(&c)
					for res, q := range l {
						// because the pod is running on the node already, all resource names must be covered
						total := resList[res]
						total.Sub(q)
						resList[res] = total
					}
				}
			}

			// see if we fulfilled all req
			min := replicas // minimum replicas can be fulfilled in this node, the least among all resources
			for res, q := range resources {
				// check every single resource type asked by workload
				nodeQ, exists := resList[res]
				if !exists {
					// node does not provide this resource type, skip this node
					min = 0
					break
				}

				// no div in quantity, try sub repeatedly to get the max replicas can be fulfilled for this resource on this node
				count := 0
				for count < replicas {
					if nodeQ.Cmp(q) != -1 {
						nodeQ.Sub(q)
					} else {
						break
					}
					count++
				}

				// if the max replicas can be fulfilled of this resource is less the other resource types, use this number
				if min > count {
					min = count
				}
			}

			if min == replicas {
				return nil
			}
			replicas = replicas - min
		}
	}
	return fmt.Errorf("%s for %d pod(s), each requiring %s", errorMessageMissingResource, replicas, prettyResourceListOutput(resources))
}

// Attention: NodeSelectorTerms in NodeSelector are ORed https://pkg.go.dev/k8s.io/api/core/v1#NodeSelector
// We generate one labelselector for each nodeselectorterm
// matchFields are not supported yet.  In order to do that, change the return type to tuple 1 for label, 1 for field
func aggregatedNodeSelectorInDeployment(podTemplate *corev1.PodTemplateSpec) []metav1.LabelSelector {
	nodeSelector := podTemplate.Spec.NodeSelector

	affinity := podTemplate.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil || affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return []metav1.LabelSelector{
			{MatchLabels: nodeSelector},
		}
	}

	selectors := []metav1.LabelSelector{}
	for _, term := range affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		selector := metav1.LabelSelector{
			MatchLabels: nodeSelector,
		}
		selector.MatchExpressions = make([]metav1.LabelSelectorRequirement, len(term.MatchExpressions))
		for i, ex := range term.MatchExpressions {
			selector.MatchExpressions[i] = metav1.LabelSelectorRequirement{
				Key:      ex.Key,
				Operator: metav1.LabelSelectorOperator(ex.Operator),
				Values:   ex.Values,
			}
		}
		selectors = append(selectors, selector)
	}
	return selectors
}

func extractPodTemplateSpecAndAggregateResourceRequirements(obj *unstructured.Unstructured) (*corev1.PodTemplateSpec, corev1.ResourceList, error) {

	// only convert the PodSpec part is better to cover other controllers (e.g. StatefulSet, DaemonSet)
	podTemplateObject, exists, err := utils.NestedField(obj.Object, controllerPodTemplatePath)
	if err != nil || !exists {
		return nil, nil, fmt.Errorf("failed to locate the replicas field in controller object. exists: %t, err: %v. Obj:%v", exists, err, obj)
	}
	podTemplateSpec := &corev1.PodTemplateSpec{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(map[string]interface{}(podTemplateObject.(map[string]interface{})), podTemplateSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse pod template from controller. err: %s. Obj:%v", err.Error(), podTemplateObject)
	}

	resources := make(corev1.ResourceList)

	for _, c := range podTemplateSpec.Spec.Containers {
		resList := getConvergedResourceListFromContainer(&c)
		for res, q := range resList {
			if _, exists := resources[res]; !exists {
				resources[res] = q
			} else {
				total := resources[res]
				total.Add(q)
				resources[res] = total
			}
		}
	}
	return podTemplateSpec, resources, nil
}

func getConvergedResourceListFromContainer(c *corev1.Container) corev1.ResourceList {
	if c == nil {
		return nil
	}

	resList := c.Resources.Requests.DeepCopy()
	for res, q := range c.Resources.Limits {
		if _, exists := resList[res]; !exists {
			resList[res] = q
		}
	}

	return resList
}
