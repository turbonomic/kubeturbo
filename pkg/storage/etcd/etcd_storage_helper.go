/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package etcd

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/vmturbo/kubeturbo/pkg/conversion"
	"github.com/vmturbo/kubeturbo/pkg/storage"
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
	"github.com/vmturbo/kubeturbo/pkg/storage/watch"

	etcdclient "github.com/coreos/etcd/client"
	etcderr "github.com/coreos/etcd/error"
	"github.com/golang/glog"
	"golang.org/x/net/context"
)

func NewEtcdStorage(client etcdclient.Client, codec conversion.Codec, prefix string) storage.Storage {
	return &etcdHelper{
		client:     client,
		codec:      codec,
		pathPrefix: prefix,
	}
}

// etcdHelper is the reference implementation of storage.Interface.
type etcdHelper struct {
	client etcdclient.Client
	codec  conversion.Codec

	// prefix for all etcd keys
	pathPrefix string
}

var ctx = context.Background()

// Codec provides access to the underlying codec being used by the implementation.
func (h *etcdHelper) Codec() conversion.Codec {
	return h.codec
}

// Implements storage.Interface.
func (h *etcdHelper) Create(key string, obj, out vmtruntime.VMTObject, ttl uint64) error {

	key = h.prefixEtcdKey(key)
	data, err := h.codec.Encode(obj)
	if err != nil {
		glog.Errorf("Error Encode: %s", err)
		return err
	}

	// startTime := time.Now()

	opts := etcdclient.SetOptions{
		TTL:       time.Duration(ttl) * time.Second,
		PrevExist: etcdclient.PrevNoExist,
	}
	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)
	response, err := etcdKeysAPI.Set(ctx, key, string(data), &opts)
	// metrics.RecordEtcdRequestLatency("create", getTypeName(obj), startTime)
	if err != nil {
		glog.Errorf("Error Create: %s", err)
		return err
	}
	if out != nil {
		if _, err := conversion.EnforcePtr(out); err != nil {
			panic("unable to convert output object to pointer")
		}
		_, _, err = h.extractObj(response, err, out, false, false)
		if err != nil {
			glog.Errorf("Error extract obj: %s", err)
		}
	}
	return err
}

// Implements storage.Interface.
func (h *etcdHelper) Update(key string, newObj vmtruntime.VMTObject, ignoreNotFound bool) error {
	key = h.prefixEtcdKey(key)

	v, err := conversion.EnforcePtr(newObj)
	if err != nil {
		// Panic is appropriate, because this is a programming error.
		panic("need ptr to type")
	}

	obj := reflect.New(v.Type()).Interface().(vmtruntime.VMTObject)
	origBody, node, res, err := h.bodyAndExtractObj(key, obj, ignoreNotFound)
	if err != nil {
		return fmt.Errorf("Error getting original object from etcd: %s", err)
	}

	index := uint64(0)
	ttl := uint64(0)
	if node != nil {
		index = node.ModifiedIndex
		if node.TTL != 0 {
			ttl = uint64(node.TTL)
		}
		if node.Expiration != nil && ttl == 0 {
			ttl = 1
		}
	} else if res != nil {
		index = res.Index
	}

	// Swap origBody with data, if origBody is the latest etcd data.
	opts := etcdclient.SetOptions{
		PrevValue: origBody,
		PrevIndex: index,
		TTL:       time.Duration(ttl) * time.Second,
	}

	data, err := h.codec.Encode(newObj)
	if err != nil {
		glog.Errorf("Error Encode: %s", err)
		return err
	}

	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)
	response, err := etcdKeysAPI.Set(ctx, key, string(data), &opts)
	// metrics.RecordEtcdRequestLatency("create", getTypeName(obj), startTime)
	if err != nil {
		glog.Errorf("Error updating: %s", err)
		return err
	}

	// if _, err := conversion.EnforcePtr(newObj); err != nil {
	// 	panic("unable to convert output object to pointer")
	// }
	_, _, err = h.extractObj(response, err, newObj, false, false)
	if err != nil {
		glog.Errorf("Error extract obj: %s", err)
	}
	return err
}

// // Implements storage.Interface.
// func (h *etcdHelper) Set(key string, obj, out runtime.Object, ttl uint64) error {
// 	var response *etcd.Response
// 	data, err := h.codec.Encode(obj)
// 	if err != nil {
// 		return err
// 	}
// 	key = h.prefixEtcdKey(key)

// 	create := true
// 	if h.versioner != nil {
// 		if version, err := h.versioner.ObjectResourceVersion(obj); err == nil && version != 0 {
// 			create = false
// 			startTime := time.Now()
// 			response, err = h.client.CompareAndSwap(key, string(data), ttl, "", version)
// 			metrics.RecordEtcdRequestLatency("compareAndSwap", getTypeName(obj), startTime)
// 			if err != nil {
// 				return err
// 			}
// 		}
// 	}
// 	if create {
// 		// Create will fail if a key already exists.
// 		startTime := time.Now()
// 		response, err = h.client.Create(key, string(data), ttl)
// 		metrics.RecordEtcdRequestLatency("create", getTypeName(obj), startTime)
// 	}

// 	if err != nil {
// 		return err
// 	}
// 	if out != nil {
// 		if _, err := conversion.EnforcePtr(out); err != nil {
// 			panic("unable to convert output object to pointer")
// 		}
// 		_, _, err = h.extractObj(response, err, out, false, false)
// 	}

// 	return err
// }

// Implements storage.Interface.
func (h *etcdHelper) Delete(key string, out vmtruntime.VMTObject) error {
	key = h.prefixEtcdKey(key)
	if _, err := conversion.EnforcePtr(out); err != nil {
		panic("unable to convert output object to pointer")
	}

	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)

	response, err := etcdKeysAPI.Delete(ctx, key, nil)
	// if !IsEtcdNotFound(err) {
	// 	// if the object that existed prior to the delete is returned by etcd, update out.
	// 	if err != nil || response.PrevNode != nil {
	// 		_, _, err = h.extractObj(response, err, out, false, true)
	// 	}
	// }
	glog.V(4).Infof("Delete response is: %v", response)
	return err
}

// Implements storage.Interface.
func (h *etcdHelper) Watch(key string, resourceVersion uint64, filter storage.FilterFunc) (watch.Interface, error) {
	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)

	key = h.prefixEtcdKey(key)
	w := newEtcdWatcher(true, false, h.codec, filter)
	exist, err := h.isKeyExist(key)
	if exist {
		go w.etcdWatch(ctx, etcdKeysAPI, key, resourceVersion)
	} else {
		return nil, err
	}
	return w, nil
}

func (h *etcdHelper) isKeyExist(key string) (bool, error) {
	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)

	_, err := etcdKeysAPI.Get(ctx, key, nil)
	if err != nil {
		if etcdError, ok := err.(*etcderr.Error); ok && etcdError != nil && etcdError.ErrorCode == etcderr.EcodeKeyNotFound {
			// glog.Errorf("watch was unable to retrieve the current index for the provided key (%q): %v", key, err)
			return false, etcdError
		}
		// if index, ok := etcdErrorIndex(err); ok {
		// 	resourceVersion = index
		// }
		// glog.Errorf("Error get intial: %v", err)

	}
	return true, nil
}

// // Implements storage.Interface.
// func (h *etcdHelper) WatchList(key string, resourceVersion uint64, filter storage.FilterFunc) (watch.Interface, error) {
// 	key = h.prefixEtcdKey(key)
// 	w := newEtcdWatcher(true, exceptKey(key), filter, h.codec, h.versioner, nil, h)
// 	go w.etcdWatch(h.client, key, resourceVersion)
// 	return w, nil
// }

// bodyAndExtractObj performs the normal Get path to etcd, returning the parsed node and response for additional information
// about the response, like the current etcd index and the ttl.
func (h *etcdHelper) bodyAndExtractObj(key string, objPtr vmtruntime.VMTObject, ignoreNotFound bool) (body string, node *etcdclient.Node, res *etcdclient.Response, err error) {
	// startTime := time.Now()
	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)

	response, err := etcdKeysAPI.Get(ctx, key, nil)
	// metrics.RecordEtcdRequestLatency("get", getTypeName(objPtr), startTime)

	// if err != nil && !IsEtcdNotFound(err) {
	// 	return "", nil, nil, err
	// }
	body, node, err = h.extractObj(response, err, objPtr, ignoreNotFound, false)
	return body, node, response, err
}

func (h *etcdHelper) extractObj(response *etcdclient.Response, inErr error, objPtr interface{}, ignoreNotFound, prevNode bool) (body string, node *etcdclient.Node, err error) {
	if response != nil {
		if prevNode {
			node = response.PrevNode
		} else {
			node = response.Node
		}
	}
	if inErr != nil || node == nil || len(node.Value) == 0 {
		if ignoreNotFound {
			v, err := conversion.EnforcePtr(objPtr)
			if err != nil {
				return "", nil, err
			}
			v.Set(reflect.Zero(v.Type()))
			return "", nil, nil
		} else if inErr != nil {
			return "", nil, inErr
		}
		return "", nil, fmt.Errorf("unable to locate a value on the response: %#v", response)
	}
	body = node.Value
	objPtr, err = h.codec.Decode([]byte(body))
	if err != nil {
		glog.Errorf("Error Decode during extractObj: %s", err)
	}
	return body, node, err
}

// Implements storage.Interface.
func (h *etcdHelper) Get(key string, objPtr vmtruntime.VMTObject, ignoreNotFound bool) error {
	key = h.prefixEtcdKey(key)
	_, _, _, err := h.bodyAndExtractObj(key, objPtr, ignoreNotFound)
	return err
}

// // Implements storage.Interface.
// func (h *etcdHelper) GetToList(key string, listObj runtime.Object) error {
// 	trace := util.NewTrace("GetToList " + getTypeName(listObj))
// 	listPtr, err := runtime.GetItemsPtr(listObj)
// 	if err != nil {
// 		return err
// 	}
// 	key = h.prefixEtcdKey(key)
// 	startTime := time.Now()
// 	trace.Step("About to read etcd node")
// 	response, err := h.client.Get(key, false, false)
// 	metrics.RecordEtcdRequestLatency("get", getTypeName(listPtr), startTime)
// 	trace.Step("Etcd node read")
// 	if err != nil {
// 		if IsEtcdNotFound(err) {
// 			return nil
// 		}
// 		return err
// 	}

// 	nodes := make([]*etcd.Node, 0)
// 	nodes = append(nodes, response.Node)

// 	if err := h.decodeNodeList(nodes, listPtr); err != nil {
// 		return err
// 	}
// 	trace.Step("Object decoded")
// 	if h.versioner != nil {
// 		if err := h.versioner.UpdateList(listObj, response.EtcdIndex); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

// decodeNodeList walks the tree of each node in the list and decodes into the specified object
func (h *etcdHelper) decodeNodeList(nodes []*etcdclient.Node, slicePtr interface{}) error {
	v, err := conversion.EnforcePtr(slicePtr)
	if err != nil || v.Kind() != reflect.Slice {
		// This should not happen at runtime.
		panic("need ptr to slice")
	}
	for _, node := range nodes {
		if node.Dir {
			if err := h.decodeNodeList(node.Nodes, slicePtr); err != nil {
				return err
			}
			continue
		}
		// obj := reflect.New(v.Type().Elem())
		value, err := h.codec.Decode([]byte(node.Value))
		obj, err := conversion.EnforcePtr(value)
		if err != nil {
			glog.Errorf("error enforce ptr for value: %v", err)
		}
		if err != nil {
			return err
		}
		// append(slicePtr, obj)
		// obj = value.(reflect.Value)
		v.Set(reflect.Append(v, obj))
		// }
	}
	return nil
}

func (h *etcdHelper) List(key string, listObj vmtruntime.VMTObject) error {
	listPtr, err := GetItemsPtr(listObj)
	if err != nil {
		glog.Errorf("Error in Etcd List after GetItemPtr %s", err)
		return err
	}
	key = h.prefixEtcdKey(key)

	nodes, _, err := h.listEtcdNode(ctx, key)

	glog.V(4).Infof("Nodes length is %d", len(nodes))

	if err != nil {
		glog.Errorf("Error in Etcd List after listEtcdNode %s", err)
		return err
	}
	if err := h.decodeNodeList(nodes, listPtr); err != nil {
		glog.Errorf("Error in Etcd List after decodeNodeList %s", err)
		return err
	}
	return nil
}

func (h *etcdHelper) listEtcdNode(ctx context.Context, key string) ([]*etcdclient.Node, uint64, error) {
	etcdKeysAPI := etcdclient.NewKeysAPI(h.client)

	opts := etcdclient.GetOptions{
		Recursive: true,
		Sort:      true,
	}

	result, err := etcdKeysAPI.Get(ctx, key, &opts)
	if err != nil {
		return nil, 0, err
	}
	return result.Node.Nodes, result.Index, nil
}

// // Implements storage.Interface.
// func (h *etcdHelper) GuaranteedUpdate(key string, ptrToType runtime.Object, ignoreNotFound bool, tryUpdate storage.UpdateFunc) error {
// 	v, err := conversion.EnforcePtr(ptrToType)
// 	if err != nil {
// 		// Panic is appropriate, because this is a programming error.
// 		panic("need ptr to type")
// 	}
// 	key = h.prefixEtcdKey(key)
// 	for {
// 		obj := reflect.New(v.Type()).Interface().(runtime.Object)
// 		origBody, node, res, err := h.bodyAndExtractObj(key, obj, ignoreNotFound)
// 		if err != nil {
// 			return err
// 		}
// 		meta := storage.ResponseMeta{}
// 		if node != nil {
// 			meta.TTL = node.TTL
// 			if node.Expiration != nil {
// 				meta.Expiration = node.Expiration
// 			}
// 			meta.ResourceVersion = node.ModifiedIndex
// 		}
// 		// Get the object to be written by calling tryUpdate.
// 		ret, newTTL, err := tryUpdate(obj, meta)
// 		if err != nil {
// 			return err
// 		}

// 		index := uint64(0)
// 		ttl := uint64(0)
// 		if node != nil {
// 			index = node.ModifiedIndex
// 			if node.TTL > 0 {
// 				ttl = uint64(node.TTL)
// 			}
// 		} else if res != nil {
// 			index = res.EtcdIndex
// 		}

// 		if newTTL != nil {
// 			ttl = *newTTL
// 		}

// 		data, err := h.codec.Encode(ret)
// 		if err != nil {
// 			return err
// 		}

// 		// First time this key has been used, try creating new value.
// 		if index == 0 {
// 			startTime := time.Now()
// 			response, err := h.client.Create(key, string(data), ttl)
// 			metrics.RecordEtcdRequestLatency("create", getTypeName(ptrToType), startTime)
// 			if IsEtcdNodeExist(err) {
// 				continue
// 			}
// 			_, _, err = h.extractObj(response, err, ptrToType, false, false)
// 			return err
// 		}

// 		if string(data) == origBody {
// 			return nil
// 		}

// 		startTime := time.Now()
// 		// Swap origBody with data, if origBody is the latest etcd data.
// 		response, err := h.client.CompareAndSwap(key, string(data), ttl, origBody, index)
// 		metrics.RecordEtcdRequestLatency("compareAndSwap", getTypeName(ptrToType), startTime)
// 		if IsEtcdTestFailed(err) {
// 			// Try again.
// 			continue
// 		}
// 		_, _, err = h.extractObj(response, err, ptrToType, false, false)
// 		return err
// 	}
// }

func (h *etcdHelper) prefixEtcdKey(key string) string {
	if strings.HasPrefix(key, path.Join("/", h.pathPrefix)) {
		return key
	}
	return path.Join("/", h.pathPrefix, key)
}

func GetItemsPtr(list vmtruntime.VMTObject) (interface{}, error) {
	v, err := conversion.EnforcePtr(list)
	if err != nil {
		glog.Errorf("EnforcePtr Error: %s", err)
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
