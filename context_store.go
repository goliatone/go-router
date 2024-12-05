package router

import (
	"sync"
)

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

func (s *safeStore) Get(key string, def any) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s, ok := s.store[key]; ok {
		return s
	}
	return def
}

func (s *safeStore) GetString(key string, def string) string {
	val := s.Get(key, def)
	str, ok := val.(string)
	if !ok {
		return def
	}
	return str
}

func (s *safeStore) GetInt(key string, def int) int {
	val := s.Get(key, def)
	num, ok := val.(int)
	if !ok {
		return def
	}
	return num
}

func (s *safeStore) GetBool(key string, def bool) bool {
	val := s.Get(key, def)
	b, ok := val.(bool)
	if !ok {
		return def
	}
	return b
}

// GetContextValue functions for type assertion
func GetContextValue[T any](c Context, key string, def T) T {
	val := c.Get(key, nil)
	if val == nil {
		return def
	}

	if typed, ok := val.(T); ok {
		return typed
	}
	return def
}
