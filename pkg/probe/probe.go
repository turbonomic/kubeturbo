package probe

import (
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/kubeturbo/pkg/helper"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

var (
	ClusterID string
)

type KubeProbe struct {
	KubeClient *client.Client
	config     *ProbeConfig
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Client, config *ProbeConfig) *KubeProbe {
	flag, err := helper.LoadTestingFlag()
	if err == nil && flag.LocalTestStitchingIP != "" {
		localTestStitchingIP = flag.LocalTestStitchingIP
	}
	if ClusterID == "" {
		id, err := getClusterID(kubeClient)
		if err != nil {
			glog.Errorf("Error trying to get cluster ID:%s", err)
		} else {
			ClusterID = id
		}

	}
	return &KubeProbe{
		KubeClient: kubeClient,
		config:     config,
	}
}

func getClusterID(kubeClient *client.Client) (string, error) {
	svc, err := kubeClient.Services("default").Get("kubernetes")
	if err != nil {
		return "", err
	}
	return string(svc.UID), nil
}

func (this *KubeProbe) ParseNode() (result []*proto.EntityDTO, err error) {
	vmtNodeGetter := NewVMTNodeGetter(this.KubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes, this.config)

	k8sNodes := nodeProbe.GetNodes(labels.Everything(), fields.Everything())

	result, err = nodeProbe.parseNodeFromK8s(k8sNodes)
	return
}

// Parse pods those are defined in namespace.
func (this *KubeProbe) ParsePod(namespace string) (result []*proto.EntityDTO, err error) {
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

func (this *KubeProbe) ParseApplication(namespace string) (result []*proto.EntityDTO, err error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication(namespace)
}

func (kubeProbe *KubeProbe) ParseService(namespace string, selector labels.Selector) (result []*proto.EntityDTO, err error) {
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
