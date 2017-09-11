package util

import (
	"sync"
	"time"
	//"github.com/golang/glog"
)

type expireCallBack func(obj interface{})

type node struct {
	obj      interface{}
	version  int64
	expire   time.Time
	callBack expireCallBack
}

type ExpirationMap struct {
	lock       sync.Mutex
	items      map[string]*node
	generation int64

	ttl time.Duration
}

func (n *node) isExpire() bool {
	if time.Now().After(n.expire) {
		return true
	}
	return false
}

func (n *node) touch(ttl time.Duration) bool {
	if n.isExpire() {
		return false
	}

	n.expire = time.Now().Add(ttl)
	return true
}

func NewExpirationMap(ttl time.Duration) *ExpirationMap {
	return &ExpirationMap{
		lock:       sync.Mutex{},
		items:      make(map[string]*node),
		generation: 0,
		ttl:        ttl,
	}
}

func (s *ExpirationMap) Add(key string, obj interface{}, fun expireCallBack) (int64, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.items[key]; ok {
		return 0, false
	}

	s.generation += 1

	item := &node{
		obj:      obj,
		version:  s.generation,
		expire:   time.Now().Add(s.ttl),
		callBack: fun,
	}
	s.items[key] = item

	return item.version, true
}

func (s *ExpirationMap) Size() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return len(s.items)
}

func (s *ExpirationMap) Del(key string, version int64) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if item, ok := s.items[key]; !ok || item.version != version {
		return false
	}

	delete(s.items, key)
	return true
}

// touch: update the timestamp to prevent expire
func (s *ExpirationMap) Touch(key string, version int64) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if item, ok := s.items[key]; ok && item.version == version {
		return item.touch(s.ttl)
	}

	return false
}

//getExpiredKeys: gather all the keys that already expire.
func (s *ExpirationMap) getExpiredKeys() []string {
	keys := []string{}
	s.lock.Lock()
	defer s.lock.Unlock()

	for key, item := range s.items {
		if item.isExpire() {
			keys = append(keys, key)
		}
	}

	return keys
}

//expireItem: call the callBack function, and delete the item
func (s *ExpirationMap) expireItem(key string) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if item, ok := s.items[key]; ok {
		if item.isExpire() {
			item.callBack(item.obj)
			delete(s.items, key)
			return true
		}
	}

	return false
}

// expireItems: expire one item a time for a lock & unlock,
//   as each item's callBack function is called synchronistically,
//   we don't want the whole store to be freezen too long.
func (s *ExpirationMap) expireItems() int {
	keys := s.getExpiredKeys()

	result := len(keys)
	for _, key := range keys {
		s.expireItem(key)
	}

	return result
}

//Run: periodically check and delete the expired items
func (s *ExpirationMap) Run(stop <-chan struct{}) {
	interval := s.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}

	for {
		select {
		case <-stop:
			break
		default:
			num := s.expireItems()
			//glog.V(4).Infof("num=%d Vs. %d\n", num, s.Size())
			if num == 0 {
				time.Sleep(interval)
			} else {
				time.Sleep(time.Second)
			}
		}
	}
}

func (s *ExpirationMap) GetTTL() time.Duration {
	return s.ttl
}
