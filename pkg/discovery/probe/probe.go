package probe

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/probe/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	// clusterID is a package level variable shared by different sub-probe.
	ClusterID string
)

const (
	labelSelectEverything = ""
	fieldSelectEverything = ""
)

type K8sProbe struct {
	stitchingManager *stitching.StitchingManager
	clusterAccessor  ClusterAccessor
	config           *ProbeConfig
}

// Create a new Kubernetes probe with the given kubeClient.
func NewK8sProbe(kubeClient *client.Clientset, config *ProbeConfig) (*K8sProbe, error) {
	// First try to get cluster ID.
	if ClusterID == "" {
		id, err := getClusterID(kubeClient)
		if err != nil {
			return nil, fmt.Errorf("Error trying to get cluster ID:%s", err)
		}
		ClusterID = id
	}
	stitchingManager := stitching.NewStitchingManager(config.StitchingPropertyType)
	return &K8sProbe{
		stitchingManager: stitchingManager,
		clusterAccessor:  NewK8sClusterAccessor(kubeClient),
		config:           config,
	}, nil
}

func getClusterID(kubeClient *client.Clientset) (string, error) {
	ns := api.NamespaceDefault
	svc, err := kubeClient.CoreV1().Services(ns).Get("kubernetes", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return string(svc.UID), nil
}

func (k8sProbe *K8sProbe) ParseNode() ([]*proto.EntityDTO, error) {
	k8sNodes, err := k8sProbe.clusterAccessor.GetNodes(labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in Kubernetes cluster: %s", err)
	}

	k8sPods, err := k8sProbe.clusterAccessor.GetPods(api.NamespaceAll, labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	nodeProbe := NewNodeProbe(k8sProbe.clusterAccessor, k8sProbe.config, k8sProbe.stitchingManager)
	return nodeProbe.parseNodeFromK8s(k8sNodes, k8sPods)
}

// Parse pods those are defined in namespace.
func (k8sProbe *K8sProbe) ParsePod(namespace string) ([]*proto.EntityDTO, error) {
	k8sPods, err := k8sProbe.clusterAccessor.GetPods(namespace, labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	podProbe := NewPodProbe(k8sProbe.clusterAccessor, k8sProbe.stitchingManager)
	return podProbe.parsePodFromK8s(k8sPods)
}

func (k8sProbe *K8sProbe) ParseApplication(namespace string) ([]*proto.EntityDTO, error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (k8sProbe *K8sProbe) ParseService(namespace string) ([]*proto.EntityDTO, error) {
	serviceList, err := k8sProbe.clusterAccessor.GetServices(namespace, labelSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Failed to get all services in Kubernetes cluster: %s", err)
	}
	endpointList, err := k8sProbe.clusterAccessor.GetEndpoints(namespace, labelSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Failed to get all endpoints in Kubernetes cluster: %s", err)
	}

	svcProbe := NewServiceProbe(k8sProbe.clusterAccessor)
	return svcProbe.ParseService(serviceList, endpointList)
}
