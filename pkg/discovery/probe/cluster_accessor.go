package probe

import (
	"fmt"
	"strconv"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/golang/glog"
)

// ClusterAccessor defines how to discover nodes from a Kubernetes cluster.
type ClusterAccessor interface {
	GetNodes(label labels.Selector, field fields.Selector) ([]*api.Node, error)
	GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error)
	GetServices(namespace string, selector labels.Selector) ([]*api.Service, error)
	GetEndpoints(namespace string, selector labels.Selector) ([]*api.Endpoints, error)
}

type K8sClusterAccessor struct {
	kubeClient *client.Client
}

func NewK8sClusterAccessor(kubeClient *client.Client) *K8sClusterAccessor {
	return &K8sClusterAccessor{
		kubeClient: kubeClient,
	}
}

// Get nodes in Kubernetes cluster based on label and field.
func (accessor *K8sClusterAccessor) GetNodes(label labels.Selector, field fields.Selector) ([]*api.Node, error) {
	listOption := &api.ListOptions{
		LabelSelector: label,
		FieldSelector: field,
	}
	nodeList, err := accessor.kubeClient.Nodes().List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Failed to get nodes from Kubernetes cluster: %s", err)
	}
	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		n := node
		nodeItems = append(nodeItems, &n)
	}
	glog.V(2).Infof("Discovering Nodes.. The cluster has ", len(nodeItems), " nodes.")
	return nodeItems, nil
}

// Get running pods match specified namespace, label and field.
// If the pod is not in Running status, ignore.
func (accessor *K8sClusterAccessor) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	listOption := &api.ListOptions{
		LabelSelector: label,
		FieldSelector: field,
	}
	podList, err := accessor.kubeClient.Pods(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error getting all the desired pods from Kubernetes cluster: %s", err)
	}
	var podItems []*api.Pod
	for _, pod := range podList.Items {
		p := pod
		if pod.Status.Phase != api.PodRunning {
			// Skip pods those are not running.
			continue
		}
		podIP := p.Status.PodIP
		podIP2PodMap[podIP] = &p
		podItems = append(podItems, &p)
	}
	glog.V(2).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems, nil
}

// Get service match specified namespace and label.
func (accessor *K8sClusterAccessor) GetServices(namespace string, selector labels.Selector) ([]*api.Service, error) {
	listOption := &api.ListOptions{
		LabelSelector: selector,
	}
	serviceList, err := accessor.kubeClient.Services(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error listing services: %s", err)
	}

	var serviceItems []*api.Service
	for _, service := range serviceList.Items {
		s := service
		serviceItems = append(serviceItems, &s)
	}

	glog.V(2).Infof("Discovering Services, now the cluster has " + strconv.Itoa(len(serviceItems)) + " services")

	return serviceItems, nil
}

// Get endpoints match specified namespace and label.
func (accessor *K8sClusterAccessor) GetEndpoints(namespace string, selector labels.Selector) ([]*api.Endpoints, error) {
	listOption := &api.ListOptions{
		LabelSelector: selector,
	}
	epList, err := accessor.kubeClient.Endpoints(namespace).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Error listing endpoints: %s", err)
	}

	var epItems []*api.Endpoints
	for _, endpoint := range epList.Items {
		ep := endpoint
		epItems = append(epItems, &ep)
	}

	return epItems, nil
}
