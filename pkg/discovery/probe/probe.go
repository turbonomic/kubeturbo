package probe

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	// clusterID is a package level variable shared by different sub-probe.
	ClusterID string
)

type K8sProbe struct {
	clusterAccessor ClusterAccessor
	config          *ProbeConfig
}

// Create a new Kubernetes probe with the given kubeClient.
func NewK8sProbe(kubeClient *client.Client, config *ProbeConfig) (*K8sProbe, error) {
	// First try to get cluster ID.
	if ClusterID == "" {
		id, err := getClusterID(kubeClient)
		if err != nil {
			return nil, fmt.Errorf("Error trying to get cluster ID:%s", err)
		}
		ClusterID = id
	}
	return &K8sProbe{
		clusterAccessor: NewK8sClusterAccessor(kubeClient),
		config:          config,
	}, nil
}

func getClusterID(kubeClient *client.Client) (string, error) {
	svc, err := kubeClient.Services("default").Get("kubernetes")
	if err != nil {
		return "", err
	}
	return string(svc.UID), nil
}

func (k8sProbe *K8sProbe) ParseNode() ([]*proto.EntityDTO, error) {
	k8sNodes, err := k8sProbe.clusterAccessor.GetNodes(labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in Kubernetes cluster: %s", err)
	}

	k8sPods, err := k8sProbe.clusterAccessor.GetPods(api.NamespaceAll, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	nodeProbe := NewNodeProbe(k8sProbe.clusterAccessor, k8sProbe.config)
	return nodeProbe.parseNodeFromK8s(k8sNodes, k8sPods)
}

// Parse pods those are defined in namespace.
func (k8sProbe *K8sProbe) ParsePod(namespace string) ([]*proto.EntityDTO, error) {
	k8sPods, err := k8sProbe.clusterAccessor.GetPods(namespace, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	podProbe := NewPodProbe(k8sProbe.clusterAccessor)
	return podProbe.parsePodFromK8s(k8sPods)
}

func (k8sProbe *K8sProbe) ParseApplication(namespace string) ([]*proto.EntityDTO, error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (k8sProbe *K8sProbe) ParseService(namespace string, selector labels.Selector) ([]*proto.EntityDTO, error) {
	serviceList, err := k8sProbe.clusterAccessor.GetServices(namespace, labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("Failed to get all services in Kubernetes cluster: %s", err)
	}
	endpointList, err := k8sProbe.clusterAccessor.GetEndpoints(namespace, labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("Failed to get all endpoints in Kubernetes cluster: %s", err)
	}

	svcProbe := NewServiceProbe(k8sProbe.clusterAccessor)
	return svcProbe.ParseService(serviceList, endpointList)
}
