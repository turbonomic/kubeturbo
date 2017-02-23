package probe

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/kubeturbo/pkg/helper"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	// clusterID is a package level variable shared by different sub-probe.
	ClusterID string
)

type KubeProbe struct {
	KubeClient *client.Client
	config     *ProbeConfig
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Client, config *ProbeConfig) (*KubeProbe, error) {
	// TODO: for local testing
	flag, err := helper.LoadTestingFlag()
	if err == nil && flag.LocalTestStitchingIP != "" {
		localTestStitchingIP = flag.LocalTestStitchingIP
	}

	// First try to get cluster ID.
	if ClusterID == "" {
		id, err := getClusterID(kubeClient)
		if err != nil {
			return nil, fmt.Errorf("Error trying to get cluster ID:%s", err)
		}
		ClusterID = id
	}
	return &KubeProbe{
		KubeClient: kubeClient,
		config:     config,
	}, nil
}

func getClusterID(kubeClient *client.Client) (string, error) {
	svc, err := kubeClient.Services("default").Get("kubernetes")
	if err != nil {
		return "", err
	}
	return string(svc.UID), nil
}

func (this *KubeProbe) ParseNode() ([]*proto.EntityDTO, error) {
	vmtNodeGetter := NewVMTNodeGetter(this.KubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes, this.config)

	k8sNodes, err := nodeProbe.GetNodes(labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error during parse nodes: %s", err)
	}
	vmtPodGetter := NewVMTPodGetter(this.KubeClient)
	podProbe := NewPodProbe(vmtPodGetter.GetPods)

	k8sPods, err := podProbe.GetPods(api.NamespaceAll, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error during parse nodes: %s", err)
	}

	return nodeProbe.parseNodeFromK8s(k8sNodes, k8sPods)
}

// Parse pods those are defined in namespace.
func (this *KubeProbe) ParsePod(namespace string) ([]*proto.EntityDTO, error) {
	vmtPodGetter := NewVMTPodGetter(this.KubeClient)
	podProbe := NewPodProbe(vmtPodGetter.GetPods)

	k8sPods, err := podProbe.GetPods(namespace, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error during parse pods: %s", err)
	}

	return podProbe.parsePodFromK8s(k8sPods)
}

func (this *KubeProbe) ParseApplication(namespace string) ([]*proto.EntityDTO, error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (kubeProbe *KubeProbe) ParseService(namespace string, selector labels.Selector) ([]*proto.EntityDTO, error) {
	vmtServiceGetter := NewVMTServiceGetter(kubeProbe.KubeClient)
	vmtEndpointGetter := NewVMTEndpointGetter(kubeProbe.KubeClient)
	svcProbe := NewServiceProbe(vmtServiceGetter.GetService, vmtEndpointGetter.GetEndpoints)

	serviceList, err := svcProbe.GetService(namespace, labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error during parse service: %s", err)
	}
	endpointList, err := svcProbe.GetEndpoints(namespace, labels.Everything())
	return svcProbe.ParseService(serviceList, endpointList)
}
