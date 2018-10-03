package util

import (
	"fmt"
	"github.com/golang/glog"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	api "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	maxIteForLatestValue = 20
)

type IPodManager interface {
	GetPodFromDisplayNameOrUUID(displayName, uuid string) (*api.Pod, error)
	CachePod(old, new *api.Pod)
}

type PodCachedManager struct {
	podCache   turbostore.ITurboCache
	podsGetter v1.PodsGetter
}

func NewPodCachedManager(podCache turbostore.ITurboCache, podsGetter v1.PodsGetter) *PodCachedManager {
	return &PodCachedManager{
		podCache:   podCache,
		podsGetter: podsGetter,
	}
}

// CachePod caches the pod name and uid with the default expiration duration
func (p *PodCachedManager) CachePod(old, new *api.Pod) {
	// Same name or uid is not normal and will not be cached.
	if old.Name == new.Name || old.UID == new.UID {
		glog.Errorf("The pods have same name or uid: old(%s,%v), new(%s,%v)", old.Name, old.UID, new.Name, new.UID)
		return
	}
	p.podCache.Set(old.Name, new.Name, 0)
	p.podCache.Set(string(old.UID), string(new.UID), 0)

	glog.V(4).Infof("Cached pod name and uid: (%s,%v) -> (%s,%v)", old.Name, old.UID, new.Name, new.UID)

	// FIXME: Need also update the entries with value as the old pod
	// E.g., three actions in a row for the same pod, the third one will get an invalid pod name as the pod has been renamed.
}

// GetPodFromDisplayNameOrUUID finds the pod by the display name from Pod entity DTO and its uuid.
func (p *PodCachedManager) GetPodFromDisplayNameOrUUID(displayName, uuid string) (*api.Pod, error) {
	namespace, name, err := podutil.ParsePodDisplayName(displayName)
	if err != nil {
		err = fmt.Errorf("Failed to parse Pod display name %s with uid %s: %v", displayName, uuid, err)
		glog.Errorf(err.Error())
		return nil, err
	}

	pod, err := p.getRunningPod(p.podsGetter.Pods(namespace), name, uuid)
	if err != nil {
		err = fmt.Errorf("Failed to get Pod by %s/%s: %v", namespace, name, err)
		glog.Errorf(err.Error())
		return nil, err
	}

	return pod, err
}

// getRunningPod finds the pod which is in Running phase by its name and uid.
func (p *PodCachedManager) getRunningPod(podClient v1.PodInterface, name, uid string) (*api.Pod, error) {
	// Try the cached name first. If it exists, it means some action was applied so it got renamed.
	// If try the original name first, it may find the pod that is in terminating process from the previous action
	// as the action doesn't check if the termination process finishes.

	// Try using the cached name because actions might have been applied to the pod.
	if cachedName, ok := p.getLatestValue(name); ok {
		glog.V(3).Infof("Using cached name %s for pod %s", cachedName, name)
		return podutil.GetPodInPhase(podClient, cachedName, api.PodRunning)
	}

	// Try using the original name if it has no cached value
	if pod, err := podutil.GetPodInPhase(podClient, name, api.PodRunning); err == nil {
		return pod, nil
	}

	// Pod is not found by name. Try using uid to get the pod (more expensive call).

	// Try using uid to search for the pod (for truncated display name issue).
	// Try using the cached uid first.
	if cachedUid, ok := p.getLatestValue(uid); ok {
		glog.V(3).Infof("Using cached uid %s for pod %s(%s)", cachedUid, name, uid)
		return podutil.GetPodInPhaseByUid(podClient, cachedUid, api.PodRunning)
	}

	// Try using the original uid if it has no cached value
	return podutil.GetPodInPhaseByUid(podClient, uid, api.PodRunning)
}

// Need to find the latest pod as there might be multiple action applied to the pod.
// E.g., If there are three actions in a row for the same pod, the third one need to get the pod name after the change made by the second action.
//       action1: pod-foo (old) => pod-foo-c (new)
//       action2: pod-foo (old) => pod-foo-c (from cache) => pod-foo-c-c (new)
//       action3: pod-foo (old) => pod-foo-c (from cache) => pod-foo-c-c (from cache again) => pod-foo-c-c-c (new)
func (p *PodCachedManager) getLatestValue(key string) (string, bool) {
	val, ok := p.podCache.Get(key)
	if !ok {
		glog.V(4).Infof("No cached value found with key %s", key)
		return "", false
	}

	// Value found. Continue to getting further value using the value found as the new key
	// Set the maximum for the iteration as in any case (e.g., with a bug), we don't want to run the loop infinitely.
	maxIte := maxIteForLatestValue
	for i := 0; i < maxIte; i++ {
		glog.V(4).Infof("(key,value)=(%s,%s)", key, val)
		key = val.(string)
		if val, ok = p.podCache.Get(key); !ok { // No new cached value found
			return key, true
		}
	}

	glog.Warningf("Reached the maximal (%d) iteration for getting the latest value with key %s", maxIte, key)

	return val.(string), true
}
