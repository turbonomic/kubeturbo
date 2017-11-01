package repository

import (
	"fmt"
	"k8s.io/client-go/pkg/api/v1"
)

// Service to create group of pods
type PodCollectionService struct {
}

func (pcService *PodCollectionService) CreatePodCollection(pods []*v1.Pod, cluster *KubeCluster,
					matchingNode string, matchingQuota string) []*v1.Pod {

	selectedPods := []*v1.Pod{}
	for _, pod := range pods {
		if matchingQuota != "" {
			quota := cluster.GetQuota(pod.Namespace)
			if quota != nil && quota.Name != matchingQuota {
				continue	//drop this pod
			}
		}
		if matchingNode != "" {
			node := cluster.GetKubeNode(pod.Spec.NodeName)
			if node != nil && node.Name != matchingNode {
				continue	// drop this pod
			}
		}

		selectedPods = append(selectedPods, pod)

	}
	return selectedPods
}

func (pcService *PodCollectionService) MapPodsByQuota(pods []*v1.Pod, cluster *KubeCluster) map[*KubeQuota][]*v1.Pod {
	podsByQuotaMap := make(map[*KubeQuota][]*v1.Pod )
	for _, pod := range pods {
		// Find quota entity for the pod if available
		ns := pod.ObjectMeta.Namespace
		quota := cluster.GetQuota(ns)

		if quota == nil {
			continue //eg. default namespace
		}

		_, exists := podsByQuotaMap[quota]
		if !exists {
			podsByQuotaMap[quota] = []*v1.Pod {}
		}
		podList := podsByQuotaMap[quota]
		podList = append(podList, pod)
		podsByQuotaMap[quota] = podList
	}
	return podsByQuotaMap
}

func (pcService *PodCollectionService) MapPodsByNode(pods []*v1.Pod, cluster *KubeCluster) map[*KubeNode][]*v1.Pod {
	podsByNodeMap := make(map[*KubeNode][]*v1.Pod )
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		node, exists := cluster.Nodes[nodeName]
		if !exists {
			fmt.Printf("Unknown pod node %s\n", node)
			continue
		}

		_, exists = podsByNodeMap[node]
		if !exists {
			podsByNodeMap[node] = []*v1.Pod {}
		}
		podList := podsByNodeMap[node]
		podList = append(podList, pod)
		podsByNodeMap[node] = podList
	}
	return podsByNodeMap
}

