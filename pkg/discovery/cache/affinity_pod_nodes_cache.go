package cache

import (
	"sync"

	v1 "k8s.io/api/core/v1"
)

type AffinityPodNodesCache struct {
	sync.RWMutex
	podNodesMap map[*v1.Pod]*v1.Node
}

func NewAffinityPodNodesCache(nodes []*v1.Node, pods []*v1.Pod) *AffinityPodNodesCache {
	return &AffinityPodNodesCache{
		podNodesMap: buildPodsNodesMap(nodes, pods),
	}
}

func (c *AffinityPodNodesCache) Load(p *v1.Pod) (value *v1.Node, ok bool) {
	c.RLock()
	defer c.RUnlock()
	if value, ok := c.podNodesMap[p]; ok {
		return value, true
	}
	return nil, false
}

func (c *AffinityPodNodesCache) Delete(p *v1.Pod) {
	c.Lock()
	defer c.Unlock()
	delete(c.podNodesMap, p)
}

func (c *AffinityPodNodesCache) Store(p *v1.Pod, n *v1.Node) {
	c.Lock()
	defer c.Unlock()
	c.podNodesMap[p] = n
}

func (c *AffinityPodNodesCache) Range(f func(key *v1.Pod, value *v1.Node)) {
	// Make a copy of the map to interate over for the range operation so that the lock can be released quickly
	c.RLock()
	copy := make(map[*v1.Pod]*v1.Node)
	for k, v := range c.podNodesMap {
		copy[k] = v
	}
	c.RUnlock()
	// Process the range operation against the copy of the cache
	for key, value := range copy {
		f(key, value)
	}
}

func buildPodsNodesMap(nodes []*v1.Node, pods []*v1.Pod) map[*v1.Pod]*v1.Node {
	nodesMap := make(map[string]*v1.Node)
	for _, currNode := range nodes {
		nodesMap[currNode.Name] = currNode
	}
	podsNodesMap := make(map[*v1.Pod]*v1.Node)
	for _, currPod := range pods {
		hostingNode, exist := nodesMap[currPod.Spec.NodeName]
		if !exist || hostingNode == nil {
			continue
		}
		podsNodesMap[currPod] = hostingNode
	}
	return podsNodesMap
}
