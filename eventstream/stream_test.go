package eventstream_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/goliatone/go-router/eventstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExactMatchScopeIsolation(t *testing.T) {
	stream := eventstream.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "")
	require.NoError(t, err)

	stream.Publish(eventstream.Scope{"tenant": "t2"}, eventstream.Event{Name: "other-tenant"})
	assertNoRecord(t, sub.Records)

	expected := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "same-tenant"})
	assert.Equal(t, expected, readRecord(t, sub.Records))
}

func TestSubscribeReplayLiveCutoverIsOrdered(t *testing.T) {
	stream := eventstream.New()
	scope := eventstream.Scope{"tenant": "t1"}

	first := stream.Publish(scope, eventstream.Event{Name: "first"})
	second := stream.Publish(scope, eventstream.Event{Name: "second"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, scope, first.Cursor)
	require.NoError(t, err)
	require.False(t, sub.CursorGap)

	third := stream.Publish(scope, eventstream.Event{Name: "third"})

	assert.Equal(t, second, readRecord(t, sub.Records))
	assert.Equal(t, third, readRecord(t, sub.Records))
}

func TestBufferSizeBoundsRetainedRecordsPerScope(t *testing.T) {
	stream := eventstream.New(eventstream.WithBufferSize(2))
	scope := eventstream.Scope{"tenant": "t1"}

	first := stream.Publish(scope, eventstream.Event{Name: "first"})
	stream.Publish(scope, eventstream.Event{Name: "second"})
	stream.Publish(scope, eventstream.Event{Name: "third"})

	stats := stream.SnapshotStats()
	assert.EqualValues(t, 2, stats.BufferedRecords)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, scope, first.Cursor)
	require.NoError(t, err)
	assert.True(t, sub.CursorGap)
	assert.Equal(t, "cursor_not_found", sub.CursorGapReason)
}

func TestUnknownCursorWithoutMatchedReplayStartsLive(t *testing.T) {
	stream := eventstream.New()
	stream.Publish(eventstream.Scope{"tenant": "t2"}, eventstream.Event{Name: "other-tenant"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "missing")
	require.NoError(t, err)
	assert.False(t, sub.CursorGap)

	expected := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "live"})
	assert.Equal(t, expected, readRecord(t, sub.Records))
}

func TestSubscribeCleanupOnCancelClosesRecords(t *testing.T) {
	stream := eventstream.New()

	ctx, cancel := context.WithCancel(context.Background())
	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "")
	require.NoError(t, err)

	closed := make(chan struct{})
	go func() {
		for range sub.Records {
		}
		close(closed)
	}()

	cancel()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("expected subscription records channel to close")
	}

	assert.Eventually(t, func() bool {
		stats := stream.SnapshotStats()
		return stats.ActiveSubscribers == 0 && stats.DropReasons["client_disconnect"] == 1
	}, time.Second, 10*time.Millisecond)
}

func TestSlowConsumerIsDroppedAndCounted(t *testing.T) {
	stream := eventstream.New(
		eventstream.WithSubscriberQueueSize(1),
		eventstream.WithBufferSize(16),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "")
	require.NoError(t, err)

	for idx := 0; idx < 32; idx++ {
		stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "burst"})
	}

	assert.Eventually(t, func() bool {
		stats := stream.SnapshotStats()
		return stats.ActiveSubscribers == 0 && stats.DropReasons["slow_consumer"] == 1
	}, time.Second, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		for {
			select {
			case _, ok := <-sub.Records:
				if !ok {
					return true
				}
			default:
				return false
			}
		}
	}, time.Second, 10*time.Millisecond)
}

func TestCanonicalScopeKeyEscapesReservedCharacters(t *testing.T) {
	stream := eventstream.New()

	first := stream.Publish(eventstream.Scope{"a": "b=c"}, eventstream.Event{Name: "first"})
	second := stream.Publish(eventstream.Scope{"a=b": "c"}, eventstream.Event{Name: "second"})

	assert.NotEqual(t, first.ScopeKey, second.ScopeKey)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"a": "b=c"}, first.Cursor)
	require.NoError(t, err)
	assert.False(t, sub.CursorGap)
	assertNoRecord(t, sub.Records)
}

func TestDeliveredRecordsAreDetachedFromRetainedState(t *testing.T) {
	stream := eventstream.New()
	scope := eventstream.Scope{"tenant": "t1"}

	published := stream.Publish(scope, eventstream.Event{
		Name:      "runtime.updated",
		Payload:   json.RawMessage(`{"ok":true}`),
		Metadata:  map[string]string{"source": "publish"},
		Timestamp: time.Now().UTC(),
	})

	published.Event.Payload[0] = '['
	published.Event.Metadata["source"] = "mutated"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, scope, "")
	require.NoError(t, err)

	live := stream.Publish(scope, eventstream.Event{
		Name:      "live",
		Payload:   json.RawMessage(`{"live":true}`),
		Metadata:  map[string]string{"kind": "live"},
		Timestamp: time.Now().UTC(),
	})

	delivered := readRecord(t, sub.Records)
	delivered.Event.Payload[0] = '['
	delivered.Event.Metadata["kind"] = "mutated"

	replaySub, err := stream.Subscribe(ctx, scope, published.Cursor)
	require.NoError(t, err)
	require.False(t, replaySub.CursorGap)

	replayed := readRecord(t, replaySub.Records)
	assert.Equal(t, live.Cursor, replayed.Cursor)
	assert.JSONEq(t, `{"live":true}`, string(replayed.Event.Payload))
	assert.Equal(t, "live", replayed.Event.Name)
	assert.Equal(t, "live", replayed.Event.Metadata["kind"])
}

func readRecord(t *testing.T, records <-chan eventstream.Record) eventstream.Record {
	t.Helper()

	select {
	case record, ok := <-records:
		if !ok {
			t.Fatal("expected record, got closed channel")
		}
		return record
	case <-time.After(time.Second):
		t.Fatal("expected record")
		return eventstream.Record{}
	}
}

func assertNoRecord(t *testing.T, records <-chan eventstream.Record) {
	t.Helper()

	select {
	case record, ok := <-records:
		if !ok {
			return
		}
		t.Fatalf("unexpected record: %+v", record)
	case <-time.After(50 * time.Millisecond):
	}
}
