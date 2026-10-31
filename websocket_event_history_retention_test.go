package router

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func assertHistoryRetainedReferences(t *testing.T, history *EventHistory, storage [][]*EventMessage, want []*EventMessage) {
	t.Helper()
	history.eventsMu.RLock()
	history.indexMu.RLock()
	defer history.eventsMu.RUnlock()
	defer history.indexMu.RUnlock()
	for _, backing := range storage {
		for _, event := range backing {
			if event != nil && !slices.Contains(want, event) {
				t.Errorf("backing storage retained removed event %q", event.ID)
				break
			}
		}
	}
}

func TestEventHistoryRetentionClear(t *testing.T) {
	for _, count := range []int{0, 1, 4} {
		t.Run(fmt.Sprintf("events=%d", count), func(t *testing.T) {
			history := NewEventHistory(4, 0)
			defer history.Close()
			var events []*EventMessage
			for i := range count {
				event := expiryEvent(fmt.Sprint(i), time.Now())
				events = append(events, event)
				history.Add(event)
			}
			snapshot := history.Get(EventHistoryFilter{})
			// Clear replaces index maps, so only history backing storage is reused.
			storage := expiryStorage(history)[:1]
			for range 2 {
				history.Clear()
				assertExpiryViews(t, history, nil)
				assertHistoryRetainedReferences(t, history, storage, nil)
				assertHistoryRetainedReferences(t, history, expiryStorage(history), nil)
			}
			if !slices.Equal(snapshot, events) {
				t.Error("Clear changed a caller-owned query snapshot")
			}
			for i, event := range snapshot {
				if event.ID != fmt.Sprint(i) {
					t.Error("Clear modified a stored event object")
				}
			}

			later := expiryEvent("later", time.Now())
			history.Add(later)
			assertExpiryViews(t, history, []*EventMessage{later})
			assertHistoryRetainedReferences(t, history, expiryStorage(history), []*EventMessage{later})
			history.Clear() // a smaller refill must not leave the old tail behind
			assertExpiryViews(t, history, nil)
			assertHistoryRetainedReferences(t, history, storage, nil)
		})
	}
}

func TestEventHistoryRetentionEviction(t *testing.T) {
	for _, maxSize := range []int{1, 3} {
		for _, spare := range []bool{false, true} {
			t.Run(fmt.Sprintf("limit=%d/spare=%t", maxSize, spare), func(t *testing.T) {
				history := NewEventHistory(maxSize, 0)
				defer history.Close()
				if spare {
					// Force append to reuse the allocation after removing its first slot.
					history.events = make([]*EventMessage, 0, maxSize+4)
				}
				var events []*EventMessage
				for i := range maxSize + 4 {
					snapshot := history.Get(EventHistoryFilter{})
					before := slices.Clone(snapshot)
					storage := expiryStorage(history)
					// Eviction is FIFO even when timestamps descend.
					event := expiryEvent(fmt.Sprint(i), time.Now().Add(-time.Duration(i)*time.Hour))
					events = append(events, event)
					history.Add(event)
					want := events[max(0, len(events)-maxSize):]
					assertExpiryViews(t, history, want)
					assertHistoryRetainedReferences(t, history, storage, want)
					assertHistoryRetainedReferences(t, history, expiryStorage(history), want)
					if !slices.Equal(snapshot, before) {
						t.Error("eviction changed a caller-owned query snapshot")
					}
				}
			})
		}
	}
}

func TestEventHistoryRetentionEvictionExpiryClear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(3, time.Minute)
		history.Close() // drive expiry explicitly
		history.events = make([]*EventMessage, 0, 6)
		storage := expiryStorage(history)[:1]
		now := time.Now()
		var events []*EventMessage
		for i, offset := range []time.Duration{time.Hour, -2 * time.Minute, time.Minute, 0, -3 * time.Minute} {
			event := expiryEvent(fmt.Sprint(i), now.Add(offset))
			events = append(events, event)
			history.Add(event)
		}
		assertExpiryViews(t, history, events[2:])
		history.cleanupOldEvents()
		assertExpiryViews(t, history, events[2:4])
		assertHistoryRetainedReferences(t, history, storage, events[2:4])
		history.Clear()
		assertExpiryViews(t, history, nil)
		// Includes the hidden prefix from eviction and tail from expiry.
		assertHistoryRetainedReferences(t, history, storage, nil)
		later := expiryEvent("later", now)
		history.Add(later)
		assertExpiryViews(t, history, []*EventMessage{later})
		assertHistoryRetainedReferences(t, history, storage, []*EventMessage{later})
	})
}

func TestEventHistoryRetentionConcurrentAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(8, time.Minute)
		history.Close()
		// Keep the same allocation through all adds so hidden evicted slots stay
		// reachable through history and can be checked after the workers join.
		history.events = make([]*EventMessage, 0, 256)
		storage := expiryStorage(history)[:1]
		start := make(chan struct{})
		var workers sync.WaitGroup
		for writer := range 2 {
			workers.Go(func() {
				<-start
				for i := range 80 {
					offset := time.Minute
					if i%2 == 0 {
						offset = -2 * time.Minute
					}
					history.Add(expiryEvent(fmt.Sprintf("%d/%d", writer, i), time.Now().Add(offset)))
				}
			})
		}
		workers.Go(func() {
			<-start
			for range 100 {
				for _, filter := range []EventHistoryFilter{{}, {Type: "event"}, {Namespace: "space"}, {ClientID: "client"}} {
					for _, event := range history.Get(filter) {
						if event == nil {
							t.Error("query snapshot contains a cleared slot")
						}
					}
				}
			}
		})
		workers.Go(func() {
			<-start
			for range 100 {
				history.Clear()
			}
		})
		workers.Go(func() {
			<-start
			for range 100 {
				history.cleanupOldEvents()
			}
		})
		close(start)
		workers.Wait()
		history.Clear()
		assertExpiryViews(t, history, nil)
		assertHistoryRetainedReferences(t, history, storage, nil)
		later := expiryEvent("later", time.Now())
		history.Add(later)
		assertExpiryViews(t, history, []*EventMessage{later})
	})
}
