package probe

import (
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

type KubeProbe struct {
	KubeClient *client.Client
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Client) *KubeProbe {
	return &KubeProbe{
		KubeClient: kubeClient,
	}
}

func (this *KubeProbe) ParseNode() (result []*sdk.EntityDTO, err error) {
	vmtNodeGetter := NewVMTNodeGetter(this.KubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes)

	k8sNodes := nodeProbe.GetNodes(labels.Everything(), fields.Everything())

	result, err = nodeProbe.parseNodeFromK8s(k8sNodes)
	return
}

// Parse pods those are defined in namespace.
func (this *KubeProbe) ParsePod(namespace string) (result []*sdk.EntityDTO, err error) {
	vmtPodGetter := NewVMTPodGetter(this.KubeClient)
	podProbe := NewPodProbe(vmtPodGetter.GetPods)

	k8sPods, err := podProbe.GetPods(namespace, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, err
	}

	result, err = podProbe.parsePodFromK8s(k8sPods)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (this *KubeProbe) ParseApplication(namespace string) (result []*sdk.EntityDTO, err error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (kubeProbe *KubeProbe) ParseService(namespace string, selector labels.Selector) (result []*sdk.EntityDTO, err error) {
	vmtServiceGetter := NewVMTServiceGetter(kubeProbe.KubeClient)
	vmtEndpointGetter := NewVMTEndpointGetter(kubeProbe.KubeClient)
	svcProbe := NewServiceProbe(vmtServiceGetter.GetService, vmtEndpointGetter.GetEndpoints)

	serviceList, err := svcProbe.GetService(namespace, labels.Everything())
	if err != nil {
		return nil, err
	}
	endpointList, err := svcProbe.GetEndpoints(namespace, labels.Everything())
	return svcProbe.ParseService(serviceList, endpointList)
}
