package kubeclient

import (
	"testing"

	set "github.com/deckarep/golang-set"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

func constuctNodes(ip string) []*v1.Node {
	var nodes [1]*v1.Node
	n1 := &v1.Node{}
	address := &v1.NodeAddress{
		Type:    v1.NodeInternalIP,
		Address: ip,
	}
	n1.Status.Addresses = []v1.NodeAddress{*address}
	nodes[0] = n1
	return nodes[:]
}

func TestKubeletClientCleanupCacheCompletely(t *testing.T) {
	kc := &KubeletClient{
		cache: make(map[string]*CacheEntry),
	}
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	kc.cache["host_2"] = entry
	assert.Equal(t, len(kc.cache), 2)
	// Test cleanup of all
	nodes := constuctNodes("host_3")
	assert.Equal(t, 2, kc.CleanupCache(nodes))
	assert.Equal(t, 0, len(kc.cache))
}

func TestKubeletClientCleanupCacheOne(t *testing.T) {
	kc := &KubeletClient{
		cache: make(map[string]*CacheEntry),
	}
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	kc.cache["host_2"] = entry
	assert.Equal(t, len(kc.cache), 2)
	// Test cleanup of one
	nodes := constuctNodes("host_1")
	assert.Equal(t, 1, kc.CleanupCache(nodes))
	assert.Equal(t, 1, len(kc.cache))
	// See if we have the right one remained
	_, ok := kc.cache["host_1"]
	assert.True(t, ok)
}

func TestKubeletClientCachebeenUsed(t *testing.T) {
	kc := &KubeletClient{
		cache: make(map[string]*CacheEntry),
	}
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	kc.cache["host_2"] = entry
	assert.False(t, kc.HasCacheBeenUsed("host_1"))
	assert.False(t, kc.HasCacheBeenUsed("host_3"))
	kc.cache["host_2"].used = true
	assert.True(t, kc.HasCacheBeenUsed("host_2"))
}

func TestKubeletClientCacheNil(t *testing.T) {
	kubeConf := &rest.Config{}
	conf := NewKubeletConfig(kubeConf)

	kc, _ := conf.Create(nil, "icr.io/cpopen/turbonomic/cpufreqgetter", "", map[string]set.Set{}, false)
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	assert.False(t, kc.HasCacheBeenUsed("host_1"))
	_, err := kc.GetSummary("host_1", "")
	assert.NotNil(t, err)
}
