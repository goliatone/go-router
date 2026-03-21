package eventstream_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router/eventstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHooksReceivePublishSubscribeAndDropEvents(t *testing.T) {
	var mu sync.Mutex
	publishes := make([]eventstream.PublishEvent, 0)
	subscribes := make([]eventstream.SubscribeEvent, 0)
	drops := make([]eventstream.DropEvent, 0)

	stream := eventstream.New(eventstream.WithHooks(eventstream.Hooks{
		OnPublish: func(event eventstream.PublishEvent) {
			mu.Lock()
			defer mu.Unlock()
			publishes = append(publishes, event)
		},
		OnSubscribe: func(event eventstream.SubscribeEvent) {
			mu.Lock()
			defer mu.Unlock()
			subscribes = append(subscribes, event)
		},
		OnDrop: func(event eventstream.DropEvent) {
			mu.Lock()
			defer mu.Unlock()
			drops = append(drops, event)
		},
	}))

	scope := eventstream.Scope{"tenant": "t1"}
	ctx, cancel := context.WithCancel(context.Background())

	sub, err := stream.Subscribe(ctx, scope, "")
	require.NoError(t, err)

	record := stream.Publish(scope, eventstream.Event{Name: "lifecycle"})
	require.Equal(t, record, readRecord(t, sub.Records))

	cancel()

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(publishes) == 1 && len(subscribes) == 1 && len(drops) == 1
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, publishes, 1)
	assert.Equal(t, "tenant=t1", publishes[0].ScopeKey)
	assert.Equal(t, "lifecycle", publishes[0].Record.Event.Name)

	require.Len(t, subscribes, 1)
	assert.Equal(t, "", subscribes[0].AfterCursor)
	assert.False(t, subscribes[0].CursorGap)

	require.Len(t, drops, 1)
	assert.Equal(t, "client_disconnect", drops[0].Reason)
}

func TestHooksRecoverFromPanics(t *testing.T) {
	stream := eventstream.New(eventstream.WithHooks(eventstream.Hooks{
		OnPublish: func(eventstream.PublishEvent) {
			panic("publish hook panic")
		},
		OnSubscribe: func(eventstream.SubscribeEvent) {
			panic("subscribe hook panic")
		},
		OnDrop: func(eventstream.DropEvent) {
			panic("drop hook panic")
		},
	}))

	ctx, cancel := context.WithCancel(context.Background())
	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "")
	require.NoError(t, err)

	record := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "safe"})
	assert.Equal(t, record, readRecord(t, sub.Records))

	cancel()

	assert.Eventually(t, func() bool {
		return stream.SnapshotStats().DropReasons["client_disconnect"] == 1
	}, time.Second, 10*time.Millisecond)
}

func TestHooksEmitCursorNotFoundDropForGapSubscriptions(t *testing.T) {
	var mu sync.Mutex
	drops := make([]eventstream.DropEvent, 0)
	subscribes := make([]eventstream.SubscribeEvent, 0)

	stream := eventstream.New(eventstream.WithHooks(eventstream.Hooks{
		OnSubscribe: func(event eventstream.SubscribeEvent) {
			mu.Lock()
			defer mu.Unlock()
			subscribes = append(subscribes, event)
		},
		OnDrop: func(event eventstream.DropEvent) {
			mu.Lock()
			defer mu.Unlock()
			drops = append(drops, event)
		},
	}))

	scope := eventstream.Scope{"tenant": "t1"}
	stream.Publish(scope, eventstream.Event{Name: "first"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, scope, "missing")
	require.NoError(t, err)
	require.True(t, sub.CursorGap)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(subscribes) == 1 && len(drops) == 1
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, subscribes, 1)
	assert.True(t, subscribes[0].CursorGap)
	assert.Equal(t, "cursor_not_found", subscribes[0].CursorGapReason)

	require.Len(t, drops, 1)
	assert.Equal(t, "cursor_not_found", drops[0].Reason)
	assert.Equal(t, "tenant=t1", drops[0].ScopeKey)
}
