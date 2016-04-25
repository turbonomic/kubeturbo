package kubeturbo

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"

	vmtcache "github.com/vmturbo/kubeturbo/pkg/cache"
	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"
)

// Meta stores VMT Metadata.
type Config struct {
	Client        *client.Client
	Meta          *vmtmeta.VMTMeta
	EtcdStorage   storage.Storage
	NodeQueue     *vmtcache.HashedFIFO
	PodQueue      *vmtcache.HashedFIFO
	VMTEventQueue *vmtcache.HashedFIFO
	// Close this to stop all reflectors
	StopEverything chan struct{}
}

// Create a vmturbo config
func NewVMTConfig(client *client.Client, etcdStorage storage.Storage, meta *vmtmeta.VMTMeta) *Config {
	config := &Config{
		Client:         client,
		Meta:           meta,
		EtcdStorage:    etcdStorage,
		NodeQueue:      vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		PodQueue:       vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		VMTEventQueue:  vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		StopEverything: make(chan struct{}),
	}

	// Watch minions.
	// Minions may be listed frequently, so provide a local up-to-date cache.
	cache.NewReflector(config.createMinionLW(), &api.Node{}, config.NodeQueue, 0).RunUntil(config.StopEverything)

	// monitor unassigned pod
	cache.NewReflector(config.createUnassignedPodLW(), &api.Pod{}, config.PodQueue, 0).RunUntil(config.StopEverything)

	// monitor vmtevents
	vmtcache.NewReflector(config.createVMTEventLW(), &registry.VMTEvent{}, config.VMTEventQueue, 0).RunUntil(config.StopEverything)

	return config
}

// Create a list and watch for node to filter out nodes those cannot be scheduled.
func (c *Config) createMinionLW() *cache.ListWatch {
	fields := fields.Set{api.NodeUnschedulableField: "false"}.AsSelector()
	return cache.NewListWatchFromClient(c.Client, "nodes", api.NamespaceAll, fields)
}

// Returns a cache.ListWatch that finds all pods that are
// already scheduled.
// This method is not used
func (c *Config) createAssignedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName!=" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))

	return cache.NewListWatchFromClient(c.Client, "pods", api.NamespaceAll, selector)
}

// Returns a cache.ListWatch that finds all pods that need to be
// scheduled.
func (c *Config) createUnassignedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName==" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))

	return cache.NewListWatchFromClient(c.Client, "pods", api.NamespaceAll, selector)
}

// VMTEvent ListWatch
func (c *Config) createVMTEventLW() *vmtcache.ListWatch {
	return vmtcache.NewListWatchFromStorage(c.EtcdStorage, "vmtevents", api.NamespaceAll, nil)
}

func parseSelectorOrDie(s string) fields.Selector {
	selector, err := fields.ParseSelector(s)
	if err != nil {
		panic(err)
	}
	return selector
}
