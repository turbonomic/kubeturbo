package kubeturbo

import (
	"github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	kubeletclient "github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
)

// Configuration created using the parameters passed to the kubeturbo service container.
type Config struct {
	tapSpec *K8sTAPServiceSpec

	StitchingPropType stitching.StitchingPropertyType

	// VMPriority: priority of VM in supplyChain definition from kubeturbo, should be less than 0;
	VMPriority int32
	// VMIsBase: Is VM is the base template from kubeturbo, when stitching with other VM probes, should be false;
	VMIsBase bool

	KubeClient    *kubeclient.Clientset
	DynamicClient dynamic.Interface
	KubeletClient *kubeletclient.KubeletClient
	CAClient      *versioned.Clientset
	// ORMClient builds operator resource mapping templates fetched from OperatorResourceMapping CR in discovery client
	// and provides the capability to update the corresponding CR for an Operator managed resource in action execution client.
	ORMClient *resourcemapping.ORMClient

	// Close this to stop all reflectors
	StopEverything chan struct{}

	DiscoveryIntervalSec int
	DiscoveryWorkers     int
	DiscoveryTimeoutSec  int
	ValidationWorkers    int
	ValidationTimeoutSec int

	DiscoverySamples           int
	DiscoverySampleIntervalSec int

	SccSupport    []string
	CAPINamespace string

	// Strategy to aggregate Container utilization data on ContainerSpec entity
	containerUtilizationDataAggStrategy string
	// Strategy to aggregate Container usage data on ContainerSpec entity
	containerUsageDataAggStrategy string

	failVolumePodMoves      bool
	updateQuotaToAllowMoves bool
	clusterAPIEnabled       bool
}

func NewVMTConfig2() *Config {
	cfg := &Config{
		StopEverything: make(chan struct{}),
	}

	return cfg
}

func (c *Config) WithKubeClient(client *kubeclient.Clientset) *Config {
	c.KubeClient = client
	return c
}

func (c *Config) WithDynamicClient(client dynamic.Interface) *Config {
	c.DynamicClient = client
	return c
}

func (c *Config) WithClusterAPIClient(client *versioned.Clientset) *Config {
	c.CAClient = client
	return c
}

func (c *Config) WithKubeletClient(client *kubeletclient.KubeletClient) *Config {
	c.KubeletClient = client
	return c
}

func (c *Config) WithORMClient(client *resourcemapping.ORMClient) *Config {
	c.ORMClient = client
	return c
}

func (c *Config) WithTapSpec(spec *K8sTAPServiceSpec) *Config {
	c.tapSpec = spec
	return c
}

// Create the StitchingPropertyType for reconciling the kubernetes cluster nodes with the infrastructure VMs
func (c *Config) UsingUUIDStitch(useUUID bool) *Config {
	stitchingPropType := stitching.IP
	if useUUID {
		stitchingPropType = stitching.UUID
	}
	c.StitchingPropType = stitchingPropType
	return c
}

func (c *Config) WithVMPriority(p int32) *Config {
	c.VMPriority = p
	return c
}

func (c *Config) WithVMIsBase(isBase bool) *Config {
	c.VMIsBase = isBase
	return c
}

func (c *Config) WithDiscoveryInterval(di int) *Config {
	c.DiscoveryIntervalSec = di
	return c
}

func (c *Config) WithValidationTimeout(di int) *Config {
	c.ValidationTimeoutSec = di
	return c
}

func (c *Config) WithValidationWorkers(di int) *Config {
	c.ValidationWorkers = di
	return c
}

func (c *Config) WithDiscoveryWorkers(workers int) *Config {
	c.DiscoveryWorkers = workers
	return c
}

func (c *Config) WithDiscoveryTimeout(timeout int) *Config {
	c.DiscoveryTimeoutSec = timeout
	return c
}

func (c *Config) WithDiscoverySamples(discoverySamples int) *Config {
	c.DiscoverySamples = discoverySamples
	return c
}

func (c *Config) WithDiscoverySampleIntervalSec(sampleIntervalSec int) *Config {
	c.DiscoverySampleIntervalSec = sampleIntervalSec
	return c
}

func (c *Config) WithSccSupport(sccSupport []string) *Config {
	c.SccSupport = sccSupport
	return c
}

func (c *Config) WithCAPINamespace(CAPINamespace string) *Config {
	c.CAPINamespace = CAPINamespace
	return c
}

func (c *Config) WithContainerUtilizationDataAggStrategy(containerUtilizationDataAggStrategy string) *Config {
	c.containerUtilizationDataAggStrategy = containerUtilizationDataAggStrategy
	return c
}

func (c *Config) WithContainerUsageDataAggStrategy(containerUsageDataAggStrategy string) *Config {
	c.containerUsageDataAggStrategy = containerUsageDataAggStrategy
	return c
}

func (c *Config) WithVolumePodMoveConfig(failVolumePodMoves bool) *Config {
	c.failVolumePodMoves = failVolumePodMoves
	return c
}

func (c *Config) WithQuotaUpdateConfig(updateQuotaToAllowMoves bool) *Config {
	c.updateQuotaToAllowMoves = updateQuotaToAllowMoves
	return c
}

func (c *Config) WithClusterAPIEnabled(clusterAPIEnabled bool) *Config {
	c.clusterAPIEnabled = clusterAPIEnabled
	return c
}
