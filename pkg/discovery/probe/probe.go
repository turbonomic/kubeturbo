package probe

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    api "k8s.io/client-go/pkg/api/v1"
    client "k8s.io/client-go/kubernetes"

	//"k8s.io/apimachinery/pkg/labels"
    //"k8s.io/apimachinery/pkg/fields"

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

type KubeProbe struct {
	KubeClient       *client.Clientset
	config           *ProbeConfig
	stitchingManager *stitching.StitchingManager
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Clientset, config *ProbeConfig) (*KubeProbe, error) {
	// First try to get cluster ID.
	if ClusterID == "" {
		id, err := getClusterID(kubeClient)
		if err != nil {
			return nil, fmt.Errorf("Error trying to get cluster ID:%s", err)
		}
		ClusterID = id
	}
	stitchingManager := stitching.NewStitchingManager(config.StitchingPropertyType)
	return &KubeProbe{
		KubeClient:       kubeClient,
		config:           config,
		stitchingManager: stitchingManager,
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

func (kubeProbe *KubeProbe) ParseNode() ([]*proto.EntityDTO, error) {
	vmtNodeGetter := NewVMTNodeGetter(kubeProbe.KubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes, kubeProbe.config, kubeProbe.stitchingManager)

	k8sNodes, err := nodeProbe.GetNodes(labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Error during parse nodes: %s", err)
	}

	vmtPodGetter := NewVMTPodGetter(kubeProbe.KubeClient)
	k8sPods, err := vmtPodGetter.GetPods(api.NamespaceAll, labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Error during parse nodes: %s", err)
	}

	return nodeProbe.parseNodeFromK8s(k8sNodes, k8sPods)
}

// Parse pods those are defined in namespace.
func (kubeProbe *KubeProbe) ParsePod(namespace string) ([]*proto.EntityDTO, error) {
	vmtPodGetter := NewVMTPodGetter(kubeProbe.KubeClient)
	podProbe := NewPodProbe(vmtPodGetter.GetPods, kubeProbe.stitchingManager)

	k8sPods, err := podProbe.GetPods(namespace, labelSelectEverything, fieldSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Error during parse pods: %s", err)
	}

	return podProbe.parsePodFromK8s(k8sPods)
}

func (this *KubeProbe) ParseApplication(namespace string) ([]*proto.EntityDTO, error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (kubeProbe *KubeProbe) ParseService(namespace string) ([]*proto.EntityDTO, error) {
	vmtServiceGetter := NewVMTServiceGetter(kubeProbe.KubeClient)
	vmtEndpointGetter := NewVMTEndpointGetter(kubeProbe.KubeClient)
	svcProbe := NewServiceProbe(vmtServiceGetter.GetService, vmtEndpointGetter.GetEndpoints)

	serviceList, err := svcProbe.GetService(namespace, labelSelectEverything)
	if err != nil {
		return nil, fmt.Errorf("Error during parse service: %s", err)
	}
	endpointList, err := svcProbe.GetEndpoints(namespace, labelSelectEverything)
	return svcProbe.ParseService(serviceList, endpointList)
}
