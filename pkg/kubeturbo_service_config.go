package kubeturbo

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	client "k8s.io/client-go/kubernetes"
)

// Configuration created using the parameters passed to the kubeturbo service container.
type Config struct {
	tapSpec *K8sTAPServiceSpec

	StitchingPropType stitching.StitchingPropertyType

	Client        *client.Clientset
	KubeletClient *kubelet.KubeletClient

	//for moveAction
	// for Kubernetes version < 1.6, schedulerName is in a different field.
	k8sVersion        string
	noneSchedulerName string

	// Flag for supporting non-disruptive action
	enableNonDisruptiveSupport bool

	// Close this to stop all reflectors
	StopEverything chan struct{}
}

func NewVMTConfig2() *Config {
	cfg := &Config{
		StopEverything: make(chan struct{}),
	}

	return cfg
}

func (c *Config) WithKubeClient(client *client.Clientset) *Config {
	c.Client = client
	return c
}

func (c *Config) WithKubeletClient(client *kubelet.KubeletClient) *Config {
	c.KubeletClient = client
	return c
}

func (c *Config) WithK8sVersion(k8sVer string) *Config {
	c.k8sVersion = k8sVer
	return c
}

func (c *Config) WithNoneScheduler(noneScheduler string) *Config {
	c.noneSchedulerName = noneScheduler
	return c
}

func (c *Config) WithTapSpec(spec *K8sTAPServiceSpec) *Config {
	c.tapSpec = spec
	return c
}

func (c *Config) WithEnableNonDisruptiveFlag(enableNonDisruptiveSupport bool) *Config {
	c.enableNonDisruptiveSupport = enableNonDisruptiveSupport
	return c
}

// Create the StitchingPropertyType for reconciling the kubernetes cluster nodes with the infrastructure VMs
func (c *Config) UsingVMWare(useVMWare bool) *Config {
	stitchingPropType := stitching.IP
	if useVMWare {
		// If the underlying hypervisor is vCenter, use UUID.
		// Refer to Bug: https://vmturbo.atlassian.net/browse/OM-18139
		stitchingPropType = stitching.UUID
	}
	c.StitchingPropType = stitchingPropType
	return c
}
