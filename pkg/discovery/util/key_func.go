package util

import (
	"strconv"
	"strings"

	"github.com/golang/glog"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	"fmt"

	api "k8s.io/api/core/v1"
)

const (
	appIdPrefix             = "App"
	controllerInfoKeyPrefix = "controllerInfo"
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
		return "", -1, fmt.Errorf("failed to parse containerId: %s", containerId)
	}

	podId := containerId[0:i]
	tmp := containerId[i+1:]
	index, err := strconv.Atoi(tmp)
	if err != nil {
		return "", -1, fmt.Errorf("failed to convert container Index[%s:%s]: %v", containerId, tmp, err)
	}

	return podId, index, nil
}

// Application's displayName = "App-namespace/podName/containerName"
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

// Construct containerSpecId based on controller UID and container name,
// e.g. "aba54eb8-3bca-11ea-bd96-005056803564/kubeturbo"
func ContainerSpecIdFunc(controllerUID string, containerName string) string {
	return controllerUID + "/" + containerName
}

func PodVolumeMetricId(podKey, volName string) string {
	volKey := podKey
	if volName != "" {
		volKey = volKey + "-" + volName
	}

	return volKey
}

func VolumeKeyFunc(vol *api.PersistentVolume) string {
	return vol.Name
}

func PodControllerInfoKey(pod *api.Pod) string {
	return fmt.Sprintf("%s-%s", controllerInfoKeyPrefix, string(pod.UID))
}
