package kubeclient

import (
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/pkg/api/v1"
	"testing"
)

func TestKubeletClientCleanupCacheCompletely(t *testing.T) {
	kc := &KubeletClient{
		cache: make(map[string]*CacheEntry),
	}
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	kc.cache["host_2"] = entry
	assert.Equal(t, len(kc.cache), 2)
	// Test cleanup of all
	var nodes [1]*v1.Node
	n1 := &v1.Node{}
	n1.Name = "host_3"
	nodes[0] = n1
	assert.Equal(t, kc.CleanupCache(nodes[:]), 2)
	assert.Equal(t, len(kc.cache), 0)
}

func TestKubeletClientCleanupCacheOne(t *testing.T) {
	kc := &KubeletClient{
		cache: make(map[string]*CacheEntry),
	}
	entry := &CacheEntry{}
	kc.cache["host_1"] = entry
	kc.cache["host_2"] = entry
	assert.Equal(t, len(kc.cache), 2)
	// Test cleanup of all
	var nodes [1]*v1.Node
	n1 := &v1.Node{}
	n1.Name = "host_1"
	nodes[0] = n1
	assert.Equal(t, kc.CleanupCache(nodes[:]), 1)
	assert.Equal(t, len(kc.cache), 1)
	// See if we have the right one remained
	_, ok := kc.cache["host_1"]
	assert.True(t, ok)
}
