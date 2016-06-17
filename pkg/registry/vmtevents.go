package registry

import (
	"fmt"

	"github.com/vmturbo/kubeturbo/pkg/storage"
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
	"github.com/vmturbo/kubeturbo/pkg/storage/watch"

	"github.com/golang/glog"
)

const (
	VMTEVENT_KEY_PREFIX string = "/vmtevents/"
)

// events implements Events interface
type VMTEventRegistry struct {
	etcdStorage storage.Storage
}

// newEvents returns a new events object.
func NewVMTEventRegistry(etcd storage.Storage) *VMTEventRegistry {
	return &VMTEventRegistry{
		etcdStorage: etcd,
	}
}

// Create makes a new vmtevent. Returns the copy of the vmtevent the server returns,
// or an error.
func (e *VMTEventRegistry) Create(event *VMTEvent) (*VMTEvent, error) {
	out, err := e.create(event)
	if err != nil {
		return nil, err
	}
	result := out.(*VMTEvent)
	return result, err
}

// Create inserts a new item according to the unique key from the object.
func (e *VMTEventRegistry) create(obj vmtruntime.VMTObject) (vmtruntime.VMTObject, error) {
	name := obj.(*VMTEvent).Name
	key := VMTEVENT_KEY_PREFIX + name
	ttl := uint64(10000)

	glog.V(5).Infof("Create vmtevent object")
	out := &VMTEvent{}
	if err := e.etcdStorage.Create(key, obj, out, ttl); err != nil {
		glog.Errorf("Error during create VMTEvent: %s", err)
		return nil, err
	}
	return out, nil
}

// Get retrieves the item from etcd.
func (e *VMTEventRegistry) Get() (vmtruntime.VMTObject, error) {
	obj := &VMTEvent{}
	key := VMTEVENT_KEY_PREFIX
	glog.Infof("Get %s", key)

	e.List()

	if err := e.etcdStorage.Get(key, obj, false); err != nil {
		return nil, err
	}
	return obj, nil
}

// List returns a list of events matching the selectors.
func (e *VMTEventRegistry) List() (*VMTEventList, error) {
	result := &VMTEventList{}
	r, err := e.ListPredicate()
	if err != nil {
		return nil, err
	}
	// glog.Infof("List(): %s", r)
	result = r.(*VMTEventList)
	return result, err

}

// ListPredicate returns a list of all the items matching m.
func (e *VMTEventRegistry) ListPredicate() (interface{}, error) {
	list := &VMTEventList{}
	rootKey := VMTEVENT_KEY_PREFIX
	err := e.etcdStorage.List(rootKey, list)
	if err != nil {
		return nil, err
	}
	return list, err
}

// Watch starts watching for vmtevents matching the given selectors.
func (e *VMTEventRegistry) Watch(resourceVersion uint64) (watch.Interface, error) {
	rootKey := VMTEVENT_KEY_PREFIX
	watch, err := e.etcdStorage.Watch(rootKey, resourceVersion, nil)
	if err != nil {
		return nil, err
	}
	return watch, nil
}

// Delete deletes an existing event.
func (e *VMTEventRegistry) Delete(name string) error {
	key := VMTEVENT_KEY_PREFIX + name
	res := &VMTEvent{}
	err := e.etcdStorage.Delete(key, res)
	if err != nil {
		glog.Errorf("Error deleting %s: %v", key, err)
	}
	return err
}

func (e *VMTEventRegistry) DeleteAll() error {
	events, err := e.List()
	if err != nil {
		return fmt.Errorf("Error listing all vmt events: %s", err)
	}
	for _, event := range events.Items {
		errDeleteSingle := e.Delete(event.Name)
		if errDeleteSingle != nil {
			return fmt.Errorf("Error delete %s: %s", event.Name, errDeleteSingle)
		}
	}
	return nil
}
