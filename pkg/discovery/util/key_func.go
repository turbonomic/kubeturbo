package util

import (
	"github.com/golang/glog"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"strconv"
	"strings"

	"fmt"
	api "k8s.io/client-go/pkg/api/v1"
)

const (
	appIdPrefix = "App"
	vdcPrefix   = "k8s-vdc"
)

func PodMetricId(pod *stats.PodReference) string {
	return pod.Namespace + "/" + pod.Name
}

func PodMetricIdAPI(pod *api.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

func ContainerMetricId(podMId string, containerName string) string {
	return podMId + "/" + containerName
}

func ApplicationMetricId(containerMId string) string {
	return appIdPrefix + "-" + containerMId
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

// Application's displayName = "App-namespace/podName"
//podFullName should be "namespace/podName"
func ApplicationDisplayName(podFullName, containerName string) string {
	return fmt.Sprintf("%s-%s/%s", appIdPrefix, podFullName, containerName)
}

func GetPodFullNameFromAppName(appName string) string {
	i := len(appIdPrefix) + 1
	if len(appName) < i+1 {
		glog.Errorf("Invalid appName: %v", appName)
		return ""
	}

	j := strings.LastIndex(appName, "/")
	if j <= i {
		glog.Errorf("Invalid appName: %v", appName)
		return ""
	}

	return appName[i:j]
}

func ApplicationIdFunc(containerId string) string {
	return fmt.Sprintf("%s-%s", appIdPrefix, containerId)
}

func ContainerIdFromApp(appId string) (string, error) {
	i := len(appIdPrefix) + 1
	if len(appId) < i+1 {
		return "", fmt.Errorf("Illegal appId:%s", appId)
	}

	return appId[i:], nil
}

func PodIdFromApp(appId string) (string, error) {
	containerId, err := ContainerIdFromApp(appId)
	if err != nil {
		return "", err
	}

	podId, _, err := ParseContainerId(containerId)
	if err != nil {
		return "", fmt.Errorf("Illegal containerId[%s] for appId[%s].", containerId, appId)
	}

	return podId, nil
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

func VDCIdFunc(namespaceId string) string {
	return fmt.Sprintf("%s-%s", vdcPrefix, namespaceId)
}
