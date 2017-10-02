package util

import (
	"fmt"
	"github.com/golang/glog"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"time"
)

// a lock for bare pods to avoid concurrent contention of actions on the same pod.
// detail of its purpose can be found: https://github.com/turbonomic/kubeturbo/issues/104
type LockHelper struct {
	//for the expirationMap
	emap    *ExpirationMap
	key     string
	version int64

	callback expireCallBack

	//stop Renewing
	stop       chan struct{}
	isRenewing bool
}

func NewLockHelper(podkey string, emap *ExpirationMap) (*LockHelper, error) {
	p := &LockHelper{
		key:        podkey,
		emap:       emap,
		stop:       make(chan struct{}),
		isRenewing: false,
	}

	if emap.GetTTL() < time.Second*2 {
		err := fmt.Errorf("TTL of concurrent control map should be larger than 2 seconds.")
		glog.Error(err)
		return nil, err
	}

	return p, nil
}

func (h *LockHelper) Setkey(key string) {
	h.key = key
	return
}

// the input of this callback will be h.key.
func (h *LockHelper) AcquireLock(callback expireCallBack) bool {
	if callback == nil {
		callback = func(obj interface{}) {
			h.lockCallBack()
		}
	}
	version, flag := h.emap.Add(h.key, h.key, callback)

	if !flag {
		glog.V(3).Infof("Failed to get lock for [%s]", h.key)
		return false
	}

	glog.V(4).Infof("Get lock for [%s]", h.key)
	h.version = version

	return true
}

func (h *LockHelper) Trylock(timeout, interval time.Duration) error {
	err := goutil.RetryDuring(1000, timeout, interval, func() error {
		if !h.AcquireLock(nil) {
			return fmt.Errorf("TryLater")
		}
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (h *LockHelper) ReleaseLock() {
	h.emap.Del(h.key, h.version)
	h.StopRenew()
	glog.V(4).Infof("Released lock for [%s]", h.key)
}

func (h *LockHelper) lockCallBack() {
	// do nothing
	return
}

func (h *LockHelper) RenewLock() bool {
	return h.emap.Touch(h.key, h.version)
}

func (h *LockHelper) KeepRenewLock() {
	ttl := h.emap.GetTTL()
	interval := ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	h.isRenewing = true

	go func() {
		for {
			select {
			case <-h.stop:
				glog.V(3).Infof("schedulerHelper stop renewlock.")
				return
			default:
				if !h.RenewLock() {
					return
				}
				time.Sleep(interval)
				glog.V(4).Infof("schedulerHelper renewlock.")
			}
		}
	}()
}

func (h *LockHelper) StopRenew() {
	if h.isRenewing {
		h.isRenewing = false
		close(h.stop)
	}
}
