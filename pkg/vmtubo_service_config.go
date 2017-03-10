package kubeturbo

import (
	"reflect"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/record"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"

	vmtcache "github.com/vmturbo/kubeturbo/pkg/cache"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/golang/glog"
)

// Meta stores VMT Metadata.
type Config struct {
	tapSpec *K8sTAPServiceSpec

	Client      *client.Client
	EtcdStorage storage.Storage

	NodeQueue     *vmtcache.HashedFIFO
	PodQueue      *vmtcache.HashedFIFO
	VMTEventQueue *vmtcache.HashedFIFO

	// Configuration for creating Kubernetes probe
	ProbeConfig *probe.ProbeConfig

	// Recorder is the EventRecorder to use
	Recorder record.EventRecorder

	// Close this to stop all reflectors
	StopEverything chan struct{}
}

// Create a vmturbo config
func NewVMTConfig(client *client.Client, etcdStorage storage.Storage, probeConfig *probe.ProbeConfig,
	spec *K8sTAPServiceSpec) *Config {
	config := &Config{
		tapSpec:        spec,
		ProbeConfig:    probeConfig,
		Client:         client,
		EtcdStorage:    etcdStorage,
		NodeQueue:      vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		PodQueue:       vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		VMTEventQueue:  vmtcache.NewHashedFIFO(registry.VMTEventKeyFunc),
		StopEverything: make(chan struct{}),
	}

	vmtEventRegistry := registry.NewVMTEventRegistry(config.EtcdStorage)

	//delete all the vmt events
	errorDelete := vmtEventRegistry.DeleteAll()
	if errorDelete != nil {
		glog.Errorf("Error deleting all vmt events: %s", errorDelete)
	}

	// Watch minions.
	// Minions may be listed frequently, so provide a local up-to-date cache.
	// cache.NewReflector(config.createMinionLW(), &api.Node{}, config.NodeQueue, 0).RunUntil(config.StopEverything)

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
	return vmtcache.NewListWatchFromStorage(c.EtcdStorage, "vmtevents", api.NamespaceAll,
		func(obj interface{}) bool {
			vmtEvent, ok := obj.(*registry.VMTEvent)
			if !ok {
				glog.Infof("----------------Wrong Type: %v---------", reflect.TypeOf(obj))
				return false
			}
			if vmtEvent.Status == registry.Pending {
				return true
			}
			return false
		})
}

func parseSelectorOrDie(s string) fields.Selector {
	selector, err := fields.ParseSelector(s)
	if err != nil {
		panic(err)
	}
	return selector
}
