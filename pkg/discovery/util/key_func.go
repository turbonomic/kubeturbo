package util

import (
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	api "k8s.io/client-go/pkg/api/v1"
)

// PodStatsKeyFunc and PodKeyFunc should return the same value.
func PodStatsKeyFunc(podStat stats.PodStats) string {
	return podStat.PodRef.Namespace + "/" + podStat.PodRef.Name
}

func PodKeyFunc(pod *api.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

// NodeStatsKeyFunc and NodeKeyFunc should return the same value.
func NodeStatsKeyFunc(nodeStat stats.NodeStats) string {
	return nodeStat.NodeName
}

func NodeKeyFunc(node *api.Node) string {
	return node.Name
}

func NodeKeyFromPodFunc(pod *api.Pod) string {
	return pod.Spec.NodeName
}
