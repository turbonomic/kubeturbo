package util

import (
	"fmt"
	"github.com/golang/glog"
	"testing"
	"time"
)

const (
	defaultTTL = time.Second * 4
)

func printKey(obj interface{}) {
	if obj == nil {
		return
	}

	k, ok := obj.(string)
	if !ok {
		glog.Warning("not a string.")
	}

	glog.V(3).Infof("key:[%s] expired\n", k)
}

func TestExpirationMap_Whole(t *testing.T) {

	store := NewExpirationMap(defaultTTL)
	stop := make(chan struct{})

	go store.Run(stop)
	defer close(stop)

	num := 10
	for i := 0; i < num; i++ {
		key := fmt.Sprintf("k-%d", i)
		_, ok := store.Add(key, key, printKey)
		if !ok {
			t.Errorf("failed to add key:%s", key)
		}
	}

	if store.Size() != num {
		t.Errorf("wrong size %d Vs. %d", store.Size(), num)
	}

	time.Sleep(2 * defaultTTL)

	if store.Size() != 0 {
		t.Errorf("Expiration Test failed. %d Vs %d", store.Size(), 0)
	}
}

func TestExpirationMap_Add(t *testing.T) {

	store := NewExpirationMap(defaultTTL)
	stop := make(chan struct{})

	go store.Run(stop)
	defer close(stop)

	key := "hello"

	_, ok := store.Add(key, nil, printKey)
	if !ok {
		t.Errorf("Add failed.")
	}
}

func TestExpirationMap_Touch(t *testing.T) {

	store := NewExpirationMap(defaultTTL)
	stop := make(chan struct{})

	go store.Run(stop)
	defer close(stop)

	key := "hello"

	v, ok := store.Add(key, nil, printKey)
	if !ok {
		t.Errorf("Add failed.")
	}

	for i := 0; i < 10; i++ {
		flag := store.Touch(key, v)
		if !flag {
			t.Errorf("Touch test1 failed.")
		}

		time.Sleep(defaultTTL - time.Second)
	}

	if store.Size() != 1 {
		t.Errorf("Touch test2 failed.")
	}

	time.Sleep(defaultTTL * 2)
	flag := store.Touch(key, v)
	if flag {
		t.Errorf("Touch test3 failed.")
	}
}

func TestExpirationMap_Del(t *testing.T) {
	store := NewExpirationMap(defaultTTL)
	stop := make(chan struct{})

	go store.Run(stop)
	defer close(stop)

	key := "hello"

	v, ok := store.Add(key, nil, printKey)
	if !ok {
		t.Errorf("Add failed.")
	}

	flag := store.Del(key, v)
	if !flag {
		t.Errorf("Delete failed.")
	}

	flag = store.Del(key, v)
	if flag {
		t.Errorf("Delete2 failed.")
	}

	if store.Size() != 0 {
		t.Errorf("Delete failed.")
	}
}

func TestExpirationMap_DelTouch(t *testing.T) {
	store := NewExpirationMap(defaultTTL)
	stop := make(chan struct{})

	go store.Run(stop)
	defer close(stop)

	key := "hello"
	v, ok := store.Add(key, nil, printKey)
	if !ok {
		t.Errorf("Add failed.")
	}

	flag := store.Del(key, v)
	if !flag {
		t.Errorf("Delete failed.")
	}

	flag = store.Touch(key, v)
	if flag {
		t.Errorf("Touch failed.")
	}
}
