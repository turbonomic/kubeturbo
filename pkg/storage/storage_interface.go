package storage

import (
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
	"github.com/vmturbo/kubeturbo/pkg/storage/watch"
)

type Storage interface {
	Create(key string, obj, out vmtruntime.VMTObject, ttl uint64) error
	List(key string, listObj vmtruntime.VMTObject) error
	Get(key string, objPtr vmtruntime.VMTObject, ignoreNotFound bool) error
	Update(key string, newObj vmtruntime.VMTObject, ignoreNotFound bool) error
	Delete(key string, out vmtruntime.VMTObject) error
	Watch(key string, resourceVersion uint64, filter FilterFunc) (watch.Interface, error)
}

// FilterFunc is a predicate which takes an API object and returns true
// iff the object should remain in the set.
type FilterFunc func(obj interface{}) bool
