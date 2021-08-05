/*
Copyright 2016 The Kubernetes Authors.

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

package estimator

import (
	"sort"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/simulator"
	"k8s.io/autoscaler/cluster-autoscaler/utils/scheduler"
	klog "k8s.io/klog/v2"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// podInfo contains Pod and score that corresponds to how important it is to handle the pod first.
type podInfo struct {
	score float64
	pod   *apiv1.Pod
}

// BinpackingNodeEstimator estimates the number of needed nodes to handle the given amount of pods.
type BinpackingNodeEstimator struct {
	predicateChecker simulator.PredicateChecker
	clusterSnapshot  simulator.ClusterSnapshot
}

// NewBinpackingNodeEstimator builds a new BinpackingNodeEstimator.
func NewBinpackingNodeEstimator(
	predicateChecker simulator.PredicateChecker,
	clusterSnapshot simulator.ClusterSnapshot) *BinpackingNodeEstimator {
	return &BinpackingNodeEstimator{
		predicateChecker: predicateChecker,
		clusterSnapshot:  clusterSnapshot,
	}
}

// Estimate implements First Fit Decreasing bin-packing approximation algorithm.
// See https://en.wikipedia.org/wiki/Bin_packing_problem for more details.
// While it is a multi-dimensional bin packing (cpu, mem, ports) in most cases the main dimension
// will be cpu thus the estimated overprovisioning of 11/9 * optimal + 6/9 should be
// still be maintained.
// It is assumed that all pods from the given list can fit to nodeTemplate.
// Returns the number of nodes needed to accommodate all pods from the list.
func (estimator *BinpackingNodeEstimator) Estimate(
	pods []*apiv1.Pod,
	nodeTemplate *schedulerframework.NodeInfo) int {
	podInfos := calculatePodScore(pods, nodeTemplate)
	sort.Slice(podInfos, func(i, j int) bool { return podInfos[i].score > podInfos[j].score })

	newNodeNames := make(map[string]bool)

	if err := estimator.clusterSnapshot.Fork(); err != nil {
		klog.Errorf("Error while calling ClusterSnapshot.Fork; %v", err)
		return 0
	}
	defer func() {
		if err := estimator.clusterSnapshot.Revert(); err != nil {
			klog.Fatalf("Error while calling ClusterSnapshot.Revert; %v", err)
		}
	}()

	newNodeNameIndex := 0

	for _, podInfo := range podInfos {
		found := false

		nodeName, err := estimator.predicateChecker.FitsAnyNodeMatching(estimator.clusterSnapshot, podInfo.pod, func(nodeInfo *schedulerframework.NodeInfo) bool {
			return newNodeNames[nodeInfo.Node().Name]
		})
		if err == nil {
			found = true
			if err := estimator.clusterSnapshot.AddPod(podInfo.pod, nodeName); err != nil {
				klog.Errorf("Error adding pod %v.%v to node %v in ClusterSnapshot; %v", podInfo.pod.Namespace, podInfo.pod.Name, nodeName, err)
				return 0
			}
		}

		if !found {
			// Add new node
			newNodeName, err := estimator.addNewNodeToSnapshot(nodeTemplate, newNodeNameIndex)
			if err != nil {
				klog.Errorf("Error while adding new node for template to ClusterSnapshot; %v", err)
				return 0
			}
			newNodeNameIndex++
			// And schedule pod to it
			if err := estimator.clusterSnapshot.AddPod(podInfo.pod, newNodeName); err != nil {
				klog.Errorf("Error adding pod %v.%v to node %v in ClusterSnapshot; %v", podInfo.pod.Namespace, podInfo.pod.Name, newNodeName, err)
				return 0
			}
			newNodeNames[newNodeName] = true
		}
	}
	return len(newNodeNames)
}

func (estimator *BinpackingNodeEstimator) addNewNodeToSnapshot(
	template *schedulerframework.NodeInfo,
	nameIndex int) (string, error) {

	newNodeInfo := scheduler.DeepCopyTemplateNode(template, nameIndex)
	var pods []*apiv1.Pod
	for _, podInfo := range newNodeInfo.Pods {
		pods = append(pods, podInfo.Pod)
	}
	if err := estimator.clusterSnapshot.AddNodeWithPods(newNodeInfo.Node(), pods); err != nil {
		return "", err
	}
	return newNodeInfo.Node().Name, nil
}

// Calculates score for all pods and returns podInfo structure.
// Score is defined as cpu_sum/node_capacity + mem_sum/node_capacity.
// Pods that have bigger requirements should be processed first, thus have higher scores.
func calculatePodScore(pods []*apiv1.Pod, nodeTemplate *schedulerframework.NodeInfo) []*podInfo {
	podInfos := make([]*podInfo, 0, len(pods))

	for _, pod := range pods {
		cpuSum := resource.Quantity{}
		memorySum := resource.Quantity{}

		for _, container := range pod.Spec.Containers {
			if request, ok := container.Resources.Requests[apiv1.ResourceCPU]; ok {
				cpuSum.Add(request)
			}
			if request, ok := container.Resources.Requests[apiv1.ResourceMemory]; ok {
				memorySum.Add(request)
			}
		}
		score := float64(0)
		if cpuAllocatable, ok := nodeTemplate.Node().Status.Allocatable[apiv1.ResourceCPU]; ok && cpuAllocatable.MilliValue() > 0 {
			score += float64(cpuSum.MilliValue()) / float64(cpuAllocatable.MilliValue())
		}
		if memAllocatable, ok := nodeTemplate.Node().Status.Allocatable[apiv1.ResourceMemory]; ok && memAllocatable.Value() > 0 {
			score += float64(memorySum.Value()) / float64(memAllocatable.Value())
		}

		podInfos = append(podInfos, &podInfo{
			score: score,
			pod:   pod,
		})
	}
	return podInfos
}
