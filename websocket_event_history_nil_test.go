package router

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestEventHistoryNilAdd(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int
		closed bool
	}{
		{name: "empty"},
		{name: "partial", count: 1},
		{name: "full", count: 2},
		{name: "closed-full", count: 2, closed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				defer func() {
					if failure := recover(); failure != nil {
						t.Fatalf("nil addition or subsequent history operation panicked: %v", failure)
					}
				}()
				history := NewEventHistory(2, 0)
				defer history.Close()
				var events []*EventMessage
				for i := range test.count {
					event := expiryEvent(fmt.Sprint(i), time.Now())
					events = append(events, event)
					history.Add(event)
				}
				if test.closed {
					history.Close()
				}
				snapshot := history.Get(EventHistoryFilter{})
				history.Add(nil)
				var missing *EventMessage
				history.Add(missing)
				assertExpiryViews(t, history, events)
				if !slices.Equal(snapshot, events) {
					t.Error("nil addition changed a caller-owned query snapshot")
				}
				var replayed, wantIDs []string
				for _, event := range events {
					wantIDs = append(wantIDs, event.ID)
				}
				client := lifecycleClient{sendJSON: func(value any) error {
					event, ok := value.(*EventMessage)
					if !ok || event == nil {
						t.Errorf("unexpected replay value %v", value)
						return nil
					}
					replayed = append(replayed, event.ID)
					return nil
				}}
				if err := history.Replay(client, EventHistoryFilter{}); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(replayed, wantIDs) {
					t.Errorf("replayed %v, want %v", replayed, wantIDs)
				}
				later := expiryEvent("later", time.Now())
				history.Add(later)
				want := append(slices.Clone(events), later)
				want = want[max(0, len(want)-2):]
				assertExpiryViews(t, history, want)
				assertHistoryRetainedReferences(t, history, expiryStorage(history), want)
				history.Clear()
				history.Add(nil)
				assertExpiryViews(t, history, nil)
				history.Add(later)
				assertExpiryViews(t, history, []*EventMessage{later})
			})
		})
	}
}

func TestEventHistoryNilCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer func() {
			if failure := recover(); failure != nil {
				t.Fatalf("nil addition or subsequent cleanup operation panicked: %v", failure)
			}
		}()
		history := NewEventHistory(2, 30*time.Second)
		defer history.Close()
		history.Add(expiryEvent("expires", time.Now()))
		history.Add(nil)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		assertExpiryViews(t, history, nil)
		history.Add(nil)
		later := expiryEvent("later", time.Now())
		history.Add(later)
		history.Close()
		requireCleanupStopped(t, history.cleanup)
		history.Add(nil)
		time.Sleep(time.Minute)
		assertExpiryViews(t, history, []*EventMessage{later})
	})
}

func TestEventHistoryNilConcurrentAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(3, time.Minute)
		history.Close() // explicit sweeps run alongside the writers
		events := make([]*EventMessage, 100)
		for i := range events {
			events[i] = expiryEvent(fmt.Sprint(i), time.Now())
		}
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				<-start
				for range 100 {
					history.Add(nil)
				}
			})
		}
		workers.Go(func() {
			<-start
			for _, event := range events {
				history.Add(event)
			}
		})
		workers.Go(func() {
			<-start
			for range 100 {
				for _, filter := range []EventHistoryFilter{{}, {Type: "event"}, {Namespace: "space"}, {ClientID: "client"}} {
					for _, event := range history.Get(filter) {
						if event == nil {
							t.Error("nil addition poisoned a query snapshot")
						}
					}
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
		assertExpiryViews(t, history, events[len(events)-3:])
		history.Clear()
		history.Add(nil)
		assertExpiryViews(t, history, nil)
	})
}
