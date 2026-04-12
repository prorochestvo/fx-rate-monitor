package threadsafe

import (
	"fmt"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

func NewCache(defaultExpiration time.Duration) *Cache {
	return &Cache{c: cache.New(defaultExpiration, defaultExpiration>>2)}
}

type Cache struct {
	c *cache.Cache
	m sync.Mutex
}

func (s *Cache) Fetch(key string) (interface{}, error) {
	s.m.Lock()
	defer s.m.Unlock()

	s.c.DeleteExpired()

	val, found := s.c.Get(key)
	if !found {
		err := fmt.Errorf("key %s not found in cache", key)
		return nil, err
	}

	return val, nil
}

func (s *Cache) Pull(key string) (interface{}, error) {
	s.m.Lock()
	defer s.m.Unlock()

	s.c.DeleteExpired()

	val, found := s.c.Get(key)
	if !found {
		err := fmt.Errorf("key %s not found in cache", key)
		return nil, err
	}

	s.c.Delete(key)

	return val, nil
}

func (s *Cache) Push(key string, val interface{}) error {
	s.m.Lock()
	defer s.m.Unlock()

	s.c.DeleteExpired()

	return s.c.Add(key, val, cache.DefaultExpiration)
}
