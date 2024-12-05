package router

import (
	"sync"
	"testing"
)

func TestContextStore_Basic(t *testing.T) {
	store := NewContextStore()

	tests := []struct {
		key      string
		value    any
		expected any
	}{
		{"string", "value", "value"},
		{"int", 42, 42},
		{"bool", true, true},
		{"nil", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			store.Set(tt.key, tt.value)
			got := store.Get(tt.key, "")
			if got != tt.expected {
				t.Errorf("Get(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestContextStore_GetString(t *testing.T) {
	store := NewContextStore()

	tests := []struct {
		key      string
		value    any
		def      string
		expected string
	}{
		{"string", "value", "", "value"},
		{"int", 42, "forty-two", "forty-two"},
		{"nil", nil, "nil", "nil"},
		{"nonexistent", nil, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if tt.value != nil {
				store.Set(tt.key, tt.value)
			}
			got := store.GetString(tt.key, tt.def)
			if got != tt.expected {
				t.Errorf("GetString(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestContextStore_GetInt(t *testing.T) {
	store := NewContextStore()

	tests := []struct {
		key      string
		value    any
		def      int
		expected int
	}{
		{"string", "value", -1, -1},
		{"int", 42, 0, 42},
		{"nil", nil, 33, 33},
		{"nonexistent", nil, -2, -2},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if tt.value != nil {
				store.Set(tt.key, tt.value)
			}
			got := store.GetInt(tt.key, tt.def)
			if got != tt.expected {
				t.Errorf("GetBool(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestContextStore_GetBool(t *testing.T) {
	store := NewContextStore()

	tests := []struct {
		key      string
		value    any
		def      bool
		expected bool
	}{
		{"string", "value", true, true},
		{"int", 42, true, true},
		{"nil", nil, true, true},
		{"nonexistent", nil, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if tt.value != nil {
				store.Set(tt.key, tt.value)
			}
			got := store.GetBool(tt.key, tt.def)
			if got != tt.expected {
				t.Errorf("GetBool(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestContextStore_ThreadSafety(t *testing.T) {
	store := NewContextStore()
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // Writers and readers

	// Writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				store.Set("key", id)
			}
		}(i)
	}

	// Readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = store.Get("key", "")
			}
		}()
	}

	wg.Wait()
}

func TestContextStore_MultipleKeys(t *testing.T) {
	store := NewContextStore()
	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "key" + string(rune(j))
				store.Set(key, id)
				_ = store.Get(key, "")
			}
		}(i)
	}

	wg.Wait()
}

func TestContextStore_NilStore(t *testing.T) {
	store := &safeStore{}

	if val := store.Get("test", nil); val != nil {
		t.Errorf("Expected nil for uninitialized store, got %v", val)
	}

	if str := store.GetString("test", ""); str != "" {
		t.Errorf("Expected empty string for uninitialized store, got %v", str)
	}

	// Should not panic
	store.Set("test", "value")
}
