package old

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/golang/glog"
)

// ClusterAccessor defines how to discover nodes from a Kubernetes cluster.
type ClusterAccessor interface {
	GetNodes(label string, field string) ([]*api.Node, error)
	GetPods(namespace string, label string, field string) ([]*api.Pod, error)
	GetServices(namespace string, selector string) ([]*api.Service, error)
	GetEndpoints(namespace string, selector string) ([]*api.Endpoints, error)
}

type K8sClusterAccessor struct {
	kubeClient *client.Clientset
}

func NewK8sClusterAccessor(kubeClient *client.Clientset) *K8sClusterAccessor {
	return &K8sClusterAccessor{
		kubeClient: kubeClient,
	}
}

// Get nodes in Kubernetes cluster based on label and field.
func (accessor *K8sClusterAccessor) GetNodes(label string, field string) ([]*api.Node, error) {
	listOption := &metav1.ListOptions{
		LabelSelector: label,
		FieldSelector: field,
	}
	nodeList, err := accessor.kubeClient.CoreV1().Nodes().List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("Failed to get nodes from Kubernetes cluster: %s", err)
	}
	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		n := node
		nodeItems = append(nodeItems, &n)
	}
	glog.V(2).Infof("Discovering Nodes.. The cluster has %d nodes.", len(nodeItems))
	return nodeItems, nil
}

// Get running pods match specified namespace, label and field.
// If the pod is not in Running status, ignore.
func (accessor *K8sClusterAccessor) GetPods(namespace string, label string, field string) ([]*api.Pod, error) {
	listOption := &metav1.ListOptions{
		LabelSelector: label,
		FieldSelector: field,
	}
	podList, err := accessor.kubeClient.CoreV1().Pods(namespace).List(*listOption)
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
		podItems = append(podItems, &p)
	}
	glog.V(2).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems, nil
}

// Get service match specified namespace and label.
func (accessor *K8sClusterAccessor) GetServices(namespace string, selector string) ([]*api.Service, error) {
	listOption := &metav1.ListOptions{
		LabelSelector: selector,
	}
	serviceList, err := accessor.kubeClient.CoreV1().Services(namespace).List(*listOption)
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
func (accessor *K8sClusterAccessor) GetEndpoints(namespace string, selector string) ([]*api.Endpoints, error) {
	listOption := &metav1.ListOptions{
		LabelSelector: selector,
	}
	epList, err := accessor.kubeClient.CoreV1().Endpoints(namespace).List(*listOption)
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
