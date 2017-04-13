package util

import (
	"k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/stats"

	"k8s.io/kubernetes/pkg/api"
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
