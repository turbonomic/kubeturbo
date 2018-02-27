package kubeturbo

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	client "k8s.io/client-go/kubernetes"
)

// Configuration created using the parameters passed to the kubeturbo service container.
type Config struct {
	tapSpec *K8sTAPServiceSpec

	StitchingPropType stitching.StitchingPropertyType

	Client        *client.Clientset
	KubeletClient *kubeclient.KubeletClient

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

func (c *Config) WithKubeletClient(client *kubeclient.KubeletClient) *Config {
	c.KubeletClient = client
	return c
}

func (c *Config) WithTapSpec(spec *K8sTAPServiceSpec) *Config {
	c.tapSpec = spec
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
