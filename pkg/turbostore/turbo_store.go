package turbostore

import (
	"sync"
)

// Not Scalable and Single Point Failure.
type TurboStore struct {
	items map[string]interface{}

	lock sync.RWMutex
}

func NewTurboStore() *TurboStore {
	return &TurboStore{
		items: make(map[string]interface{}),
	}
}

func (s *TurboStore) Add(key string, obj interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.items[key] = obj
}

func (s *TurboStore) Delete(key string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.items, key)
	return nil
}

func (s *TurboStore) Get(key string) (interface{}, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	item, exist := s.items[key]
	return item, exist

}
