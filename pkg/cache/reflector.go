// /*
// Copyright 2014 The Kubernetes Authors All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package cache

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"reflect"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"k8s.io/kubernetes/pkg/api/meta"
	"k8s.io/kubernetes/pkg/client/cache"
	utilruntime "k8s.io/kubernetes/pkg/util/runtime"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/vmturbo/kubeturbo/pkg/conversion"
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
	"github.com/vmturbo/kubeturbo/pkg/storage/watch"

	etcderr "github.com/coreos/etcd/error"
	"github.com/golang/glog"
)

// ListerWatcher is any object that knows how to perform an initial list and start a watch on a resource.
type ListerWatcher interface {
	// List should return a list type object; the Items field will be extracted, and the
	// ResourceVersion field will be used to start the watch in the right place.
	List() (vmtruntime.VMTObject, error)
	// Watch should begin a watch at the specified version.
	Watch(resourceVersion string) (watch.Interface, error)
}

// Reflector watches a specified resource and causes all changes to be reflected in the given store.
type Reflector struct {
	// name identifies this reflector.  By default it will be a file:line if possible.
	name string

	// The type of object we expect to place in the store.
	expectedType reflect.Type
	// The destination to sync up with the watch source
	store cache.Store
	// listerWatcher is used to perform lists and watches.
	listerWatcher ListerWatcher
	// period controls timing between one watch ending and
	// the beginning of the next one.
	period       time.Duration
	resyncPeriod time.Duration
	// lastSyncResourceVersion is the resource version token last
	// observed when doing a sync with the underlying store
	// it is thread safe, but not synchronized with the underlying store
	lastSyncResourceVersion string
	// lastSyncResourceVersionMutex guards read/write access to lastSyncResourceVersion
	lastSyncResourceVersionMutex sync.RWMutex
}

// NewNamespaceKeyedIndexerAndReflector creates an Indexer and a Reflector
// The indexer is configured to key on namespace
func NewNamespaceKeyedIndexerAndReflector(lw ListerWatcher, expectedType interface{}, resyncPeriod time.Duration) (indexer cache.Indexer, reflector *Reflector) {
	indexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	reflector = NewReflector(lw, expectedType, indexer, resyncPeriod)
	return indexer, reflector
}

// NewReflector creates a new Reflector object which will keep the given store up to
// date with the server's contents for the given resource. Reflector promises to
// only put things in the store that have the type of expectedType.
// If resyncPeriod is non-zero, then lists will be executed after every resyncPeriod,
// so that you can use reflectors to periodically process everything as well as
// incrementally processing the things that change.
func NewReflector(lw ListerWatcher, expectedType interface{}, store cache.Store, resyncPeriod time.Duration) *Reflector {
	return NewNamedReflector(getDefaultReflectorName(internalPackages...), lw, expectedType, store, resyncPeriod)
}

// NewNamedReflector same as NewReflector, but with a specified name for logging
func NewNamedReflector(name string, lw ListerWatcher, expectedType interface{}, store cache.Store, resyncPeriod time.Duration) *Reflector {
	r := &Reflector{
		name:          name,
		listerWatcher: lw,
		store:         store,
		expectedType:  reflect.TypeOf(expectedType),
		period:        time.Second,
		resyncPeriod:  resyncPeriod,
	}
	return r
}

// internalPackages are packages that ignored when creating a default reflector name.  These packages are in the common
// call chains to NewReflector, so they'd be low entropy names for reflectors
var internalPackages = []string{"kubernetes/pkg/client/cache/", "kubernetes/pkg/controller/framework/"}

// getDefaultReflectorName walks back through the call stack until we find a caller from outside of the ignoredPackages
// it returns back a shortpath/filename:line to aid in identification of this reflector when it starts logging
func getDefaultReflectorName(ignoredPackages ...string) string {
	name := "????"
outer:
	for i := 1; i < 10; i++ {
		_, file, line, ok := goruntime.Caller(i)
		if !ok {
			break
		}
		for _, ignoredPackage := range ignoredPackages {
			if strings.Contains(file, ignoredPackage) {
				continue outer
			}

		}

		pkgLocation := strings.LastIndex(file, "/pkg/")
		if pkgLocation >= 0 {
			file = file[pkgLocation+1:]
		}
		name = fmt.Sprintf("%s:%d", file, line)
		break
	}

	return name
}

// Run starts a watch and handles watch events. Will restart the watch if it is closed.
// Run starts a goroutine and returns immediately.
func (r *Reflector) Run() {
	go wait.Until(func() { r.ListAndWatch(wait.NeverStop) }, r.period, wait.NeverStop)
}

// RunUntil starts a watch and handles watch events. Will restart the watch if it is closed.
// RunUntil starts a goroutine and returns immediately. It will exit when stopCh is closed.
func (r *Reflector) RunUntil(stopCh <-chan struct{}) {
	go wait.Until(func() { r.ListAndWatch(stopCh) }, r.period, stopCh)
}

var (
	// nothing will ever be sent down this channel
	neverExitWatch <-chan time.Time = make(chan time.Time)

	// Used to indicate that watching stopped so that a resync could happen.
	errorResyncRequested = errors.New("resync channel fired")

	// Used to indicate that watching stopped because of a signal from the stop
	// channel passed in from a client of the reflector.
	errorStopRequested = errors.New("Stop requested")
)

// resyncChan returns a channel which will receive something when a resync is
// required, and a cleanup function.
func (r *Reflector) resyncChan() (<-chan time.Time, func() bool) {
	if r.resyncPeriod == 0 {
		return neverExitWatch, func() bool { return false }
	}
	// The cleanup function is required: imagine the scenario where watches
	// always fail so we end up listing frequently. Then, if we don't
	// manually stop the timer, we could end up with many timers active
	// concurrently.
	t := time.NewTimer(r.resyncPeriod)
	return t.C, t.Stop
}

// Returns error if ListAndWatch didn't even tried to initialize watch.
func (r *Reflector) ListAndWatch(stopCh <-chan struct{}) error {
	var resourceVersion string
	resyncCh, cleanup := r.resyncChan()
	defer cleanup()

	// list, err := r.listerWatcher.List()
	// if err != nil {
	// 	glog.Errorf("%s: Failed to list %v: %v", r.name, r.expectedType, err)
	// 	return fmt.Errorf("%s: Failed to list %v: %v", r.name, r.expectedType, err)
	// }
	// glog.Infof("The list from listerWatcher is %v", list)
	// meta, err := meta.Accessor(list)
	// if err != nil {
	// 	glog.Errorf("%s: Unable to understand list result %#v", r.name, list)
	// 	return fmt.Errorf("%s: Unable to understand list result %#v", r.name, list)
	// }
	// glog.Infof("meta is %v", meta)
	// resourceVersion = meta.ResourceVersion()
	// items, err := ExtractList(list)
	// // items := list.([]interface{})

	// if err != nil {
	// 	glog.Errorf("%s: Unable to understand list result %#v (%v)", r.name, list, err)
	// 	return fmt.Errorf("%s: Unable to understand list result %#v (%v)", r.name, list, err)
	// }
	// if err := r.syncWith(items, resourceVersion); err != nil {
	// 	glog.Errorf("%s: Unable to sync list result: %v", r.name, err)
	// 	return fmt.Errorf("%s: Unable to sync list result: %v", r.name, err)
	// }
	// r.setLastSyncResourceVersion(resourceVersion)

	resourceVersion = "0"

	for {
		// glog.Info("in for")
		w, err := r.listerWatcher.Watch(resourceVersion)
		// glog.Infof("error from listerWather is %v", err)

		if err != nil {
			if etcdError, ok := err.(*etcderr.Error); ok && etcdError != nil && etcdError.ErrorCode == etcderr.EcodeKeyNotFound {
				// glog.Errorf("Key not found Error: %v", err)
				time.Sleep(time.Second)
				continue
			}
			switch err {
			case io.EOF:
				// watch closed normally
			case io.ErrUnexpectedEOF:
				glog.V(1).Infof("%s: Watch for %v closed with unexpected EOF: %v", r.name, r.expectedType, err)
			default:
				utilruntime.HandleError(fmt.Errorf("%s: Failed to watch %v: %v", r.name, r.expectedType, err))
			}
			// If this is "connection refused" error, it means that most likely apiserver is not responsive.
			// It doesn't make sense to re-list all objects because most likely we will be able to restart
			// watch where we ended.
			// If that's the case wait and resend watch request.
			if urlError, ok := err.(*url.Error); ok {
				if opError, ok := urlError.Err.(*net.OpError); ok {
					if errno, ok := opError.Err.(syscall.Errno); ok && errno == syscall.ECONNREFUSED {
						glog.Warningf("Sleep, Zzzzzzzzzzzzzz")
						time.Sleep(time.Second)
						continue
					}
				}
			}
			glog.Errorf("Error during calling watch: %v", err)
			return nil
		}
		glog.V(5).Infof("watch from listerWather is %v", w)

		if err := r.watchHandler(w, &resourceVersion, resyncCh, stopCh); err != nil {
			if err != errorResyncRequested && err != errorStopRequested {
				// glog.Warningf("%s: watch of %v ended with: %v", r.name, r.expectedType, err)
			}
			// glog.Error("Error calling watchHandler: %s. Return from for loop in ListAndWatch.", err)
			return nil
		}
		glog.V(4).Info("End of for loop in ListAndWatch.")
	}
}

// syncWith replaces the store's items with the given list.
func (r *Reflector) syncWith(items []interface{}, resourceVersion string) error {
	found := make([]interface{}, 0, len(items))
	for _, item := range items {
		found = append(found, item)
	}

	// myStore, ok := r.store.(*cache.WatchCache)
	// if ok {
	// 	return myStore.ReplaceWithVersion(found, resourceVersion)
	// }
	return r.store.Replace(found, resourceVersion)
}

// watchHandler watches w and keeps *resourceVersion up to date.
func (r *Reflector) watchHandler(w watch.Interface, resourceVersion *string, resyncCh <-chan time.Time, stopCh <-chan struct{}) error {
	start := time.Now()
	eventCount := 0

	// Stopping the watcher should be idempotent and if we return from this function there's no way
	// we're coming back in with the same watch interface.
	defer w.Stop()

loop:
	for {
		// glog.Info("Inside for loop of watchHandler")
		select {
		case <-stopCh:
			return errorStopRequested
		case <-resyncCh:
			return errorResyncRequested
		case event, ok := <-w.ResultChan():
			// glog.V(4).Infof("%v Result Event in reflector is: %v", ok, event)
			if !ok {
				break loop
			}
			if event.Type == watch.Error {
				// return apierrs.FromObject(event.Object)
				return fmt.Errorf("Error during watching")
			}
			if e, a := r.expectedType, reflect.TypeOf(event.Object); e != a {
				utilruntime.HandleError(fmt.Errorf("%s: expected type %v, but watch event object had type %v", r.name, e, a))
				continue
			}
			meta, err := meta.Accessor(event.Object)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("%s: unable to understand watch event %#v", r.name, event))
				continue
			}
			newResourceVersion := meta.GetResourceVersion()
			switch event.Type {
			case watch.Added:
				r.store.Add(event.Object)
			case watch.Modified:
				r.store.Update(event.Object)
			case watch.Deleted:
				// TODO: Will any consumers need access to the "last known
				// state", which is passed in event.Object? If so, may need
				// to change this.
				r.store.Delete(event.Object)
			default:
				utilruntime.HandleError(fmt.Errorf("%s: unable to understand watch event %#v", r.name, event))
			}
			*resourceVersion = newResourceVersion
			r.setLastSyncResourceVersion(newResourceVersion)
			eventCount++
		}
	}

	watchDuration := time.Now().Sub(start)
	if watchDuration < 1*time.Second && eventCount == 0 {
		// glog.V(4).Infof("%s: Unexpected watch close - watch lasted less than a second and no items received", r.name)
		return errors.New("very short watch")
	}
	glog.V(4).Infof("%s: Watch close - %v total %v items received", r.name, r.expectedType, eventCount)
	return nil
}

// LastSyncResourceVersion is the resource version observed when last sync with the underlying store
// The value returned is not synchronized with access to the underlying store and is not thread-safe
func (r *Reflector) LastSyncResourceVersion() string {
	r.lastSyncResourceVersionMutex.RLock()
	defer r.lastSyncResourceVersionMutex.RUnlock()
	return r.lastSyncResourceVersion
}

func (r *Reflector) setLastSyncResourceVersion(v string) {
	r.lastSyncResourceVersionMutex.Lock()
	defer r.lastSyncResourceVersionMutex.Unlock()
	r.lastSyncResourceVersion = v
}

// ExtractList returns obj's Items element as an array of runtime.Objects.
// Returns an error if obj is not a List type (does not have an Items member).
// TODO: move me to pkg/api/meta
func ExtractList(obj interface{}) ([]interface{}, error) {
	itemsPtr, err := GetItemsPtr(obj)
	if err != nil {
		return nil, err
	}
	items, err := conversion.EnforcePtr(itemsPtr)
	if err != nil {
		return nil, err
	}
	list := make([]interface{}, items.Len())
	for i := range list {
		raw := items.Index(i)
		var found bool
		switch raw.Kind() {
		case reflect.Interface, reflect.Ptr:
			list[i], found = raw.Interface().(interface{})
		default:
			list[i], found = raw.Addr().Interface().(interface{})
		}
		if !found {
			return nil, fmt.Errorf("item[%v]: Expected object, got %#v(%s)", i, raw.Interface(), raw.Kind())
		}
	}
	return list, nil
}

func GetItemsPtr(list interface{}) (interface{}, error) {
	v, err := conversion.EnforcePtr(list)
	if err != nil {
		return nil, err
	}
	items := v.FieldByName("Items")
	if !items.IsValid() {
		return nil, fmt.Errorf("no Items field in %#v", list)
	}
	switch items.Kind() {
	case reflect.Interface, reflect.Ptr:
		target := reflect.TypeOf(items.Interface()).Elem()
		if target.Kind() != reflect.Slice {
			return nil, fmt.Errorf("items: Expected slice, got %s", target.Kind())
		}
		return items.Interface(), nil
	case reflect.Slice:
		return items.Addr().Interface(), nil
	default:
		return nil, fmt.Errorf("items: Expected slice, got %s", items.Kind())
	}
}
