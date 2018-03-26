package turbostore

import (
	gocache "github.com/patrickmn/go-cache"
	"time"
)

const (
	defaultCleanUpInterval = time.Minute
)

type ITurboCache interface {
	// Add an item to the cache, replacing any existing item. If the duration is 0
	// (DefaultExpiration), the cache's default expiration time is used. If it is -1
	// (NoExpiration), the item never expires.
	Set(k string, x interface{}, d time.Duration)

	// Get an item from the cache. Returns the item or nil, and a bool indicating
	// whether the key was found.
	Get(k string) (interface{}, bool)
}

type TurboCache struct {
	Cache *gocache.Cache
}

func NewTurboCache(defaultExpiration time.Duration) *TurboCache {
	return &TurboCache{
		Cache: gocache.New(defaultExpiration, defaultCleanUpInterval),
	}
}
