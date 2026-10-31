package router

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func expiryEvent(id string, timestamp time.Time) *EventMessage {
	return &EventMessage{
		ID: id, Type: "event", Namespace: "space", Timestamp: timestamp,
		Metadata: map[string]any{"clientID": "client"},
	}
}

func assertExpiryViews(t *testing.T, history *EventHistory, want []*EventMessage) {
	t.Helper()
	if got := history.Size(); got != len(want) {
		t.Errorf("size = %d, want %d", got, len(want))
	}
	for _, filter := range []EventHistoryFilter{
		{}, {Type: "event"}, {Namespace: "space"}, {ClientID: "client"},
	} {
		if got := history.Get(filter); !slices.Equal(got, want) {
			t.Errorf("filter %+v returned the wrong event sequence (got %d events, want %d)", filter, len(got), len(want))
		}
	}
	history.indexMu.RLock()
	defer history.indexMu.RUnlock()
	if len(want) == 0 && (len(history.byType) != 0 || len(history.byNamespace) != 0 || len(history.byClient) != 0) {
		t.Error("empty history retained index buckets")
	}
}

func expiryStorage(history *EventHistory) [][]*EventMessage {
	history.eventsMu.RLock()
	history.indexMu.RLock()
	defer history.eventsMu.RUnlock()
	defer history.indexMu.RUnlock()
	storage := [][]*EventMessage{history.events[:cap(history.events)]}
	for _, index := range []map[string][]*EventMessage{history.byType, history.byNamespace, history.byClient} {
		for _, events := range index {
			storage = append(storage, events[:cap(events)])
		}
	}
	return storage
}

func assertNoExpiredReferences(t *testing.T, history *EventHistory, storage [][]*EventMessage, expired []*EventMessage) {
	t.Helper()
	history.eventsMu.RLock()
	history.indexMu.RLock()
	defer history.eventsMu.RUnlock()
	defer history.indexMu.RUnlock()
	for _, backing := range storage {
		for _, event := range backing {
			if event != nil && slices.Contains(expired, event) {
				t.Errorf("backing storage retained expired event %q", event.ID)
				break
			}
		}
	}
}

func TestEventHistoryExpirySweep(t *testing.T) {
	cases := []struct {
		name     string
		ttl      time.Duration
		offsets  []time.Duration
		keepFrom int
	}{
		{name: "empty", ttl: time.Minute},
		{name: "one-expired", ttl: time.Minute, offsets: []time.Duration{-2 * time.Minute}, keepFrom: 1},
		{name: "all-expired", ttl: time.Minute, offsets: []time.Duration{-3 * time.Minute, -2 * time.Minute}, keepFrom: 2},
		{name: "none-expired", ttl: time.Minute, offsets: []time.Duration{-30 * time.Second, 0}},
		{name: "expired-prefix", ttl: time.Minute, offsets: []time.Duration{-2 * time.Minute, -30 * time.Second, 0}, keepFrom: 1},
		{name: "cutoff-equality", ttl: time.Minute, offsets: []time.Duration{-time.Minute, -time.Minute + time.Nanosecond}, keepFrom: 1},
		{name: "zero-ttl", offsets: []time.Duration{-3 * time.Minute, -2 * time.Minute}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				history := NewEventHistory(10, test.ttl)
				history.Close() // drive explicit sweeps without a background tick
				events := make([]*EventMessage, 0, len(test.offsets))
				for i, offset := range test.offsets {
					event := expiryEvent(fmt.Sprint(i), time.Now().Add(offset))
					events = append(events, event)
					history.Add(event)
				}
				storage := expiryStorage(history)
				want := events[test.keepFrom:]
				for range 2 {
					history.cleanupOldEvents()
					assertExpiryViews(t, history, want)
				}
				assertNoExpiredReferences(t, history, storage, events[:test.keepFrom])

				var replayed []string
				client := lifecycleClient{sendJSON: func(value any) error {
					event, ok := value.(*EventMessage)
					if !ok {
						t.Errorf("unexpected replay type %T", value)
						return nil
					}
					replayed = append(replayed, event.ID)
					return nil
				}}
				if err := history.Replay(client, EventHistoryFilter{}); err != nil {
					t.Fatal(err)
				}
				var wantIDs []string
				for _, event := range want {
					wantIDs = append(wantIDs, event.ID)
				}
				if !slices.Equal(replayed, wantIDs) {
					t.Errorf("replayed %v, want %v", replayed, wantIDs)
				}

				later := expiryEvent("later", time.Now())
				history.Add(later)
				want = append(slices.Clone(want), later)
				assertExpiryViews(t, history, want)
			})
		})
	}
}

func TestEventHistoryExpiryIndexBuckets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(10, time.Minute)
		history.Close()
		old := &EventMessage{
			ID: "old", Type: "old-type", Namespace: "old-space",
			Metadata: map[string]any{"clientID": "old-client"}, Timestamp: time.Now().Add(-2 * time.Minute),
		}
		fresh := expiryEvent("fresh", time.Now())
		history.Add(old)
		history.Add(fresh)
		storage := expiryStorage(history)
		history.cleanupOldEvents()
		assertExpiryViews(t, history, []*EventMessage{fresh})
		for _, filter := range []EventHistoryFilter{
			{Type: old.Type}, {Namespace: old.Namespace}, {ClientID: "old-client"},
		} {
			if got := history.Get(filter); len(got) != 0 {
				t.Errorf("expired bucket %+v returned %v", filter, got)
			}
		}
		for name, index := range map[string]map[string][]*EventMessage{
			"type": history.byType, "namespace": history.byNamespace, "client": history.byClient,
		} {
			if len(index) != 1 {
				t.Errorf("%s index has %d buckets, want one live bucket", name, len(index))
			}
		}
		assertNoExpiredReferences(t, history, storage, []*EventMessage{old})
	})
}

func TestEventHistoryExpiryPeriodicDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(10, 30*time.Second)
		defer history.Close()
		for cycle := range 2 {
			event := expiryEvent(fmt.Sprint(cycle), time.Now())
			history.Add(event)
			storage := expiryStorage(history)
			synctest.Wait()
			time.Sleep(time.Minute)
			synctest.Wait()
			assertExpiryViews(t, history, nil)
			assertNoExpiredReferences(t, history, storage, []*EventMessage{event})
		}
	})
}

func TestEventHistoryExpiryConcurrentAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(1000, time.Hour)
		history.Close()
		start := make(chan struct{})
		var workers sync.WaitGroup
		for writer := range 3 {
			workers.Go(func() {
				<-start
				for i := range 100 {
					history.Add(expiryEvent(fmt.Sprintf("%d/%d", writer, i), time.Now().Add(-2*time.Hour)))
				}
			})
		}
		workers.Go(func() {
			<-start
			for range 100 {
				for _, filter := range []EventHistoryFilter{
					{}, {Type: "event"}, {Namespace: "space"}, {ClientID: "client"},
				} {
					history.Get(filter)
				}
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
		history.cleanupOldEvents()
		assertExpiryViews(t, history, nil)
		for _, event := range history.events[:cap(history.events)] {
			if event != nil {
				t.Error("drained history retained an event in backing storage")
				break
			}
		}
		later := expiryEvent("later", time.Now())
		history.Add(later)
		assertExpiryViews(t, history, []*EventMessage{later})
	})
}
