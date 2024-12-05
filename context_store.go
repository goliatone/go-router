package router

import "sync"

type safeStore struct {
	mu    sync.RWMutex
	store map[string]any
}

func NewContextStore() ContextStore {
	return &safeStore{
		store: make(map[string]any),
	}
}

func (s *safeStore) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		s.store = make(map[string]any)
	}
	s.store[key] = value
}

func (s *safeStore) Get(key string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store[key]
}

func (c *safeStore) GetString(key string, def string) string {
	if v := c.Get(key); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func (c *safeStore) GetInt(key string, def int) int {
	if v := c.Get(key); v != nil {
		if s, ok := v.(int); ok {
			return s
		}
	}
	return def
}

func (c *safeStore) GetBool(key string, def bool) bool {
	if v := c.Get(key); v != nil {
		if s, ok := v.(bool); ok {
			return s
		}
	}
	return def
}
