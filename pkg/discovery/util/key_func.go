package util

import (
	"github.com/golang/glog"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"strconv"

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

func ParseContainerId(containerId string) (string, int, error) {
	i := len(containerId) - 2

	for ; i >= 0; i-- {
		if containerId[i] == '-' {
			break
		}
	}

	if i < 1 {
		err := fmt.Errorf("failed to parse containerId: %s.", containerId)
		glog.Error(err)
		return "", -1, err
	}

	podId := containerId[0:i]
	tmp := containerId[i+1:]
	index, err := strconv.Atoi(tmp)
	if err != nil {
		rerr := fmt.Errorf("failed to convert container Index[%s:%s]: %v", containerId, tmp, err)
		glog.Error(rerr)
		return "", -1, rerr
	}

	return podId, index, nil
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
