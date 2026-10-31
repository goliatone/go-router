package router

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

var eventHistoryCapacityCases = []struct {
	name  string
	size  int
	limit int
}{
	{name: "zero", size: 0, limit: 1000},
	{name: "negative", size: -1, limit: 1000},
	{name: "minimum-int", size: math.MinInt, limit: 1000},
	{name: "one", size: 1, limit: 1},
	{name: "positive", size: 3, limit: 3},
}

func TestEventHistoryCapacity(t *testing.T) {
	for _, test := range eventHistoryCapacityCases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if failure := recover(); failure != nil {
					t.Fatalf("history capacity %d panicked: %v", test.size, failure)
				}
			}()
			history := NewEventHistory(test.size, 0)
			defer history.Close()
			if history.cleanup != nil {
				t.Error("capacity normalization started cleanup with zero TTL")
			}
			var events []*EventMessage
			for i := range test.limit + 2 {
				event := expiryEvent(fmt.Sprint(i), time.Now())
				events = append(events, event)
				history.Add(event)
			}
			assertExpiryViews(t, history, events[2:])
			history.Clear()
			assertExpiryViews(t, history, nil)
			history.Close()
			later := expiryEvent("later", time.Now())
			history.Add(later)
			assertExpiryViews(t, history, []*EventMessage{later})
		})
	}
}

func TestEventRouterHistoryCapacity(t *testing.T) {
	for _, test := range eventHistoryCapacityCases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if failure := recover(); failure != nil {
					t.Fatalf("router history capacity %d panicked: %v", test.size, failure)
				}
			}()
			router := NewEventRouter(EventRouterConfig{EnableHistory: true, MaxHistorySize: test.size})
			defer router.Close()
			if router.config.MaxHistorySize != test.limit {
				t.Errorf("configured capacity = %d, want %d", router.config.MaxHistorySize, test.limit)
			}
			if router.history == nil {
				t.Fatal("capacity normalization disabled history")
			}
			var events []*EventMessage
			for i := range test.limit + 2 {
				event := expiryEvent(fmt.Sprint(i), time.Now())
				events = append(events, event)
				if err := router.RouteEvent(context.Background(), lifecycleClient{}, event); err != nil {
					t.Fatal(err)
				}
			}
			if got := router.GetHistory(EventHistoryFilter{}); !slices.Equal(got, events[2:]) {
				t.Error("routed history did not retain the bounded FIFO sequence")
			}
			assertExpiryViews(t, router.history, events[2:])
		})
	}
	t.Run("disabled", func(t *testing.T) {
		router := NewEventRouter(EventRouterConfig{MaxHistorySize: -1})
		defer router.Close()
		if router.history != nil {
			t.Fatal("capacity normalization enabled history")
		}
		if err := router.RouteEvent(context.Background(), lifecycleClient{}, expiryEvent("ignored", time.Now())); err != nil {
			t.Fatal(err)
		}
		if got := router.GetHistory(EventHistoryFilter{}); len(got) != 0 {
			t.Error("disabled history retained a routed event")
		}
	})
}

func TestEventHistoryCapacityCleanup(t *testing.T) {
	for _, size := range []int{0, -1, math.MinInt} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				defer func() {
					if failure := recover(); failure != nil {
						t.Fatalf("history capacity %d panicked: %v", size, failure)
					}
				}()
				history := NewEventHistory(size, 30*time.Second)
				defer history.Close()
				history.Add(expiryEvent("expires", time.Now()))
				synctest.Wait()
				time.Sleep(time.Minute)
				synctest.Wait()
				assertExpiryViews(t, history, nil)
				history.Close()
				requireCleanupStopped(t, history.cleanup)
				later := expiryEvent("later", time.Now())
				history.Add(later)
				time.Sleep(2 * time.Minute)
				assertExpiryViews(t, history, []*EventMessage{later})
			})
		})
	}
}
