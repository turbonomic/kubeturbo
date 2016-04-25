/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	// "reflect"

	"k8s.io/kubernetes/pkg/fields"
	// "k8s.io/kubernetes/pkg/watch"

	"github.com/vmturbo/kubeturbo/pkg/storage"
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
	"github.com/vmturbo/kubeturbo/pkg/storage/watch"

	"github.com/golang/glog"
)

// ListFunc knows how to list resources
type ListFunc func() (vmtruntime.VMTObject, error)

// WatchFunc knows how to watch resources
type WatchFunc func(resourceVersion string) (watch.Interface, error)

// ListWatch knows how to list and watch a set of apiserver resources.  It satisfies the ListerWatcher interface.
// It is a convenience function for users of NewReflector, etc.
// ListFunc and WatchFunc must not be nil
type ListWatch struct {
	ListFunc  ListFunc
	WatchFunc WatchFunc
}

// NewListWatchFromClient creates a new ListWatch from the specified client, resource, namespace and field selector.
func NewListWatchFromStorage(s storage.Storage, resource string, namespace string, fieldSelector fields.Selector) *ListWatch {
	listFunc := func() (vmtruntime.VMTObject, error) {
		return nil, nil

		var list vmtruntime.VMTObject
		// glog.Infof("Type of list is %v", reflect.TypeOf(&list))
		err := s.List(resource, list)
		if err != nil {
			glog.Infof("Error listing: %v", err)
			return nil, err
		}
		glog.Infof("List Func result is :%+v", list)
		return list, nil
	}
	watchFunc := func(resourceVersion string) (watch.Interface, error) {
		return s.Watch(resource, 0, nil)
	}
	return &ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

// List a set of apiserver resources
func (lw *ListWatch) List() (vmtruntime.VMTObject, error) {
	return lw.ListFunc()
}

// Watch a set of apiserver resources
func (lw *ListWatch) Watch(resourceVersion string) (watch.Interface, error) {
	return lw.WatchFunc(resourceVersion)
}
