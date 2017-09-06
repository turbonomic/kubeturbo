package util

import (
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	"fmt"
	api "k8s.io/client-go/pkg/api/v1"
)

const (
	appIdPrefix = "app"
)

// PodStatsKeyFunc and PodKeyFunc should return the same value.
func PodStatsKeyFunc(podStat *stats.PodStats) string {
	return podStat.PodRef.Namespace + "/" + podStat.PodRef.Name
}

func ContainerStatNameFunc(pod *stats.PodStats, containerName string) string {
	return PodStatsKeyFunc(pod) + "/" + containerName
}

func PodKeyFunc(pod *api.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

func ContainerIdFunc(podId string, index int) string {
	return fmt.Sprintf("%s-%d", podId, index)
}

func ApplicationIdFunc(containerId string) string {
	return fmt.Sprintf("%s-%s", appIdPrefix, containerId)
}

func ContainerNameFunc(pod *api.Pod, container *api.Container) string {
	return PodKeyFunc(pod) + "/" + container.Name
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
