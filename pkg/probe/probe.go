package probe

import (
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "github.com/vmturbo/kubeturbo/pkg/cadvisor"
	"github.com/vmturbo/kubeturbo/pkg/helper"

	"github.com/golang/glog"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var localTestingFlag bool = false

var actionTestingFlag bool = false

var localTestStitchingIP string = ""

func init() {
	flag, err := helper.LoadTestingFlag("./pkg/helper/testing_flag.json")
	if err != nil {
		glog.Errorf("Error initialize probe package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
	actionTestingFlag = flag.ActionTestingFlag
	localTestStitchingIP = flag.LocalTestStitchingIP

	glog.V(4).Infof("Local %s", localTestingFlag)
	glog.V(4).Infof("Stitching IP %s", localTestStitchingIP)
}

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

// Show container stats for each container. For debug and troubleshooting purpose.
func showContainerStats(container *vmtAdvisor.Container) {
	glog.V(3).Infof("Host name %s", container.Hostname)
	glog.V(3).Infof("Container name is %s", container.Name)
	containerStats := container.Stats
	currentStat := containerStats[len(containerStats)-1]
	glog.V(3).Infof("CPU usage is %d", currentStat.Cpu.Usage.Total)
	glog.V(3).Infof("MEM usage is %d", currentStat.Memory.Usage)
}
