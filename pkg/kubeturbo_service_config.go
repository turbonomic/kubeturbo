package kubeturbo

import (
	"k8s.io/apimachinery/pkg/fields"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	vmtcache "github.com/turbonomic/kubeturbo/pkg/cache"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
)

// Meta stores VMT Metadata.
type Config struct {
	tapSpec *K8sTAPServiceSpec

	//turboStore *turbostore.TurboStore
	broker turbostore.Broker

	Client        *client.Clientset
	KubeletClient *kubelet.KubeletClient

	//TODO: delete these two
	// they were used for rescheduler, but they are useless now.
	NodeQueue *vmtcache.HashedFIFO
	PodQueue  *vmtcache.HashedFIFO

	// Configuration for creating Kubernetes probe
	ProbeConfig *configs.ProbeConfig

	// Recorder is the EventRecorder to use
	Recorder record.EventRecorder

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
		NodeQueue:      vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		PodQueue:       vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
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

func (c *Config) WithProbeConfig(pconfig *configs.ProbeConfig) *Config {
	c.ProbeConfig = pconfig
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

func (c *Config) WithBroker(broker turbostore.Broker) *Config {
	c.broker = broker
	return c
}

func (c *Config) WithTapSpec(spec *K8sTAPServiceSpec) *Config {
	c.tapSpec = spec
	return c
}

func (c *Config) WithRecorder(rc record.EventRecorder) *Config {
	c.Recorder = rc
	return c
}

func (c *Config) WithEnableNonDisruptiveFlag(enableNonDisruptiveSupport bool) *Config {
	c.enableNonDisruptiveSupport = enableNonDisruptiveSupport
	return c
}

func NewVMTConfig(client *client.Clientset, kubeletClient *kubelet.KubeletClient, probeConfig *configs.ProbeConfig, broker turbostore.Broker,
	spec *K8sTAPServiceSpec, k8sVer, noneScheduler string) *Config {
	config := &Config{
		tapSpec:           spec,
		broker:            broker,
		ProbeConfig:       probeConfig,
		Client:            client,
		k8sVersion:        k8sVer,
		noneSchedulerName: noneScheduler,
		NodeQueue:         vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		PodQueue:          vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		StopEverything:    make(chan struct{}),
	}

	// Watch minions.
	// Minions may be listed frequently, so provide a local up-to-date cache.
	// cache.NewReflector(config.createMinionLW(), &api.Node{}, config.NodeQueue, 0).RunUntil(config.StopEverything)

	// monitor unassigned pod ## NO USE currently @ 2017.07.20
	// cache.NewReflector(config.createUnassignedPodLW(), &api.Pod{}, config.PodQueue, 0).RunUntil(config.StopEverything)

	return config
}

// Create a list and watch for node to filter out nodes those cannot be scheduled.
func (c *Config) createMinionLW() *cache.ListWatch {
	//fields := fields.Set{api.NodeUnschedulableField: "false"}.AsSelector()
	selector := fields.ParseSelectorOrDie("spec.unschedulable == false")
	return cache.NewListWatchFromClient(c.Client.CoreV1().RESTClient(), "nodes", api.NamespaceAll, selector)
}

// Returns a cache.ListWatch that finds all pods that are
// already scheduled.
// This method is not used
func (c *Config) createAssignedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName!=" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))

	return cache.NewListWatchFromClient(c.Client.CoreV1().RESTClient(), "pods", api.NamespaceAll, selector)
}

// Returns a cache.ListWatch that finds all pods that need to be
// scheduled.
func (c *Config) createUnassignedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName==" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))

	return cache.NewListWatchFromClient(c.Client.CoreV1().RESTClient(), "pods", api.NamespaceAll, selector)
}
