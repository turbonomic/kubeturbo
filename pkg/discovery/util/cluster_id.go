package util

import (
	api "k8s.io/client-go/pkg/api/v1"
)

// A Kubernetes in-cluster unique ID is namespace/name
func BuildK8sEntityClusterID(namespace, name string) string {
	return namespace + "/" + name
}

func GetPodClusterID(pod *api.Pod) string {
	return BuildK8sEntityClusterID(pod.Namespace, pod.Name)
}

func GetServiceClusterID(service *api.Service) string {
	return BuildK8sEntityClusterID(service.Namespace, service.Name)
}

func GetEndpointsClusterID(ep *api.Endpoints) string {
	return BuildK8sEntityClusterID(ep.Namespace, ep.Name)
}
