/*
Copyright 2017 The Kubernetes Authors.

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

package scheduler

import (
	"fmt"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// CreateNodeNameToInfoMap obtains a list of pods and pivots that list into a map where the keys are node names
// and the values are the aggregated information for that node. Pods waiting lower priority pods preemption
// (pod.Status.NominatedNodeName is set) are also added to list of pods for a node.
func CreateNodeNameToInfoMap(pods []*apiv1.Pod, nodes []*apiv1.Node) map[string]*schedulerframework.NodeInfo {
	nodeNameToNodeInfo := make(map[string]*schedulerframework.NodeInfo)
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			nodeName = pod.Status.NominatedNodeName
		}
		if _, ok := nodeNameToNodeInfo[nodeName]; !ok {
			nodeNameToNodeInfo[nodeName] = schedulerframework.NewNodeInfo()
		}
		nodeNameToNodeInfo[nodeName].AddPod(pod)
	}

	for _, node := range nodes {
		if _, ok := nodeNameToNodeInfo[node.Name]; !ok {
			nodeNameToNodeInfo[node.Name] = schedulerframework.NewNodeInfo()
		}
		nodeNameToNodeInfo[node.Name].SetNode(node)
	}

	// Some pods may be out of sync with node lists. Removing incomplete node infos.
	keysToRemove := make([]string, 0)
	for key, nodeInfo := range nodeNameToNodeInfo {
		if nodeInfo.Node() == nil {
			keysToRemove = append(keysToRemove, key)
		}
	}
	for _, key := range keysToRemove {
		delete(nodeNameToNodeInfo, key)
	}

	return nodeNameToNodeInfo
}

// DeepCopyTemplateNode copies NodeInfo object used as a template. It changes
// names of UIDs of both node and pods running on it, so that copies can be used
// to represent multiple nodes.
func DeepCopyTemplateNode(nodeTemplate *schedulerframework.NodeInfo, index int) *schedulerframework.NodeInfo {
	node := nodeTemplate.Node().DeepCopy()
	node.Name = fmt.Sprintf("%s-%d", node.Name, index)
	node.UID = uuid.NewUUID()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["kubernetes.io/hostname"] = node.Name
	nodeInfo := schedulerframework.NewNodeInfo()
	nodeInfo.SetNode(node)
	for _, podInfo := range nodeTemplate.Pods {
		pod := podInfo.Pod.DeepCopy()
		pod.Name = fmt.Sprintf("%s-%d", podInfo.Pod.Name, index)
		pod.UID = uuid.NewUUID()
		nodeInfo.AddPod(pod)
	}
	return nodeInfo
}
