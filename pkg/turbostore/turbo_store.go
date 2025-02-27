package turbostore

import (
	"sync"
)

// Not Scalable and Single Point Failure.
type Cache struct {
	items map[string]interface{}

	lock sync.RWMutex
}

func NewCache() *Cache {
	return &Cache{
		items: make(map[string]interface{}),
	}
}

func (s *Cache) Add(key string, obj interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.items[key] = obj
}

func (s *Cache) AddAll(entries map[string]interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for key, obj := range entries {
		s.items[key] = obj
	}
}

func (s *Cache) Delete(key string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.items, key)
	return nil
}

func (s *Cache) Get(key string) (interface{}, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	item, exist := s.items[key]
	return item, exist
}

// Get all the keys of current cache.
func (s *Cache) AllKeys() []string {
	s.lock.RLock()
	defer s.lock.RUnlock()

	keys := make([]string, 0, len(s.items))
	for k := range s.items {
		keys = append(keys, k)
	}
	return keys
}
