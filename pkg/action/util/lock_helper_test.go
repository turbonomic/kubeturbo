package util

import (
	// "github.com/golang/glog"
	"fmt"
	"testing"
	"time"
)

func getLockMap(ttl time.Duration, stop chan struct{}) *ExpirationMap {
	store := NewExpirationMap(ttl)
	go store.Run(stop)

	return store
}

func TestLockHelper_Acquirelock(t *testing.T) {
	ttl := time.Second * 2
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	if !helper.AcquireLock(nil) {
		t.Errorf("failed to acquire lock.")
	}

	helper.ReleaseLock()
}

func TestLockHelper_Acquirelock_expire(t *testing.T) {
	ttl := time.Second * 2
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	callback := func(obj interface{}) {
		k, ok := obj.(string)
		if !ok {
			fmt.Println("not a string.")
		}

		fmt.Printf("key[%s] expired.\n", k)
	}

	if !helper.AcquireLock(callback) {
		t.Errorf("failed to acquire lock.")
	}

	// lock will be expired.
	time.Sleep(ttl + ttl)
}

func TestLockHelper_Acquirelock2(t *testing.T) {
	ttl := time.Second * 2
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	// 1. should be able to acquire lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to acquire lock.")
	}

	// 2. should be not able to get lock
	if helper.AcquireLock(nil) {
		t.Errorf("should not be able to get lock.")
	}

	helper.ReleaseLock()
}

func TestLockHelper_Acquirelock3(t *testing.T) {
	ttl := time.Second * 2
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	// 1. should be able to acquire lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to acquire lock.")
	}

	// 2. sleep to wait lock expire
	time.Sleep(ttl + ttl)

	// 3. should be able to get lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to acquire lock.")
	}

	helper.ReleaseLock()
}

func TestLockHelper_Trylock(t *testing.T) {
	ttl := time.Second * 2
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	// 1. p1 can get lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to get lock.")
	}

	// 2. p2 try to get lock, should be able to get the lock.
	timeOut := ttl + ttl
	interval := time.Second
	if err := helper.Trylock(timeOut, interval); err != nil {
		t.Errorf("failed to acquire lock.")
	}

	//3. p3 should not be able to get lock
	if helper.AcquireLock(nil) {
		t.Errorf("Should not get lock.")
	}

	helper.ReleaseLock()
}

func TestLockHelper_Trylock2(t *testing.T) {
	ttl := time.Second * 3
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	// 1. p1 can get lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to get lock.")
	}

	// 2. p2 try to get lock, should not be able to get the lock.
	timeOut := ttl / 2
	interval := time.Second
	if err := helper.Trylock(timeOut, interval); err == nil {
		t.Errorf("should not get lock.")
	}

	helper.ReleaseLock()
}

func TestLockHelper_KeepRenewLock(t *testing.T) {
	ttl := time.Second * 3
	stop := make(chan struct{})
	store := getLockMap(ttl, stop)
	defer close(stop)

	key := "default/pod1"
	helper, _ := NewLockHelper(key, store)

	// 1. p1 can get lock
	if !helper.AcquireLock(nil) {
		t.Errorf("failed to get lock.")
	}
	helper.KeepRenewLock()

	// 2. p2 try to get lock, should not be able to get the lock.
	timeOut := ttl + ttl
	interval := time.Second
	if err := helper.Trylock(timeOut, interval); err == nil {
		t.Errorf("failed to acquire lock.")
	}

	helper.ReleaseLock()
}
