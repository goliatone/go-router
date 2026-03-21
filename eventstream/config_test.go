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

func TestNewNormalizesDefaultsAndDeliversLiveRecords(t *testing.T) {
	stream := eventstream.New(
		eventstream.WithBufferSize(0),
		eventstream.WithSubscriberQueueSize(-1),
		eventstream.WithMatcher(nil),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "")
	require.NoError(t, err)

	record := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{
		Name:    "runtime.updated",
		Payload: json.RawMessage(`{"ok":true}`),
	})

	require.Equal(t, "1", record.Cursor)
	require.Equal(t, "tenant=t1", record.ScopeKey)
	require.False(t, record.PublishedAt.IsZero())
	require.False(t, record.Event.Timestamp.IsZero())

	select {
	case delivered := <-sub.Records:
		assert.Equal(t, record, delivered)
	case <-time.After(time.Second):
		t.Fatal("expected live record")
	}

	cancel()
	assert.Eventually(t, func() bool {
		stats := stream.SnapshotStats()
		return stats.DropReasons["client_disconnect"] == 1
	}, time.Second, 10*time.Millisecond)

	stats := stream.SnapshotStats()
	assert.EqualValues(t, 1, stats.PublishedCount)
	assert.EqualValues(t, 0, stats.ResumeCount)
	assert.EqualValues(t, 1, stats.BufferedRecords)
	assert.EqualValues(t, 0, stats.ActiveSubscribers)
}

func TestSubscribeReplaysAfterKnownCursor(t *testing.T) {
	stream := eventstream.New(eventstream.WithMatcher(eventstream.SubsetMatch))

	first := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "first"})
	second := stream.Publish(eventstream.Scope{"tenant": "t1", "resource": "agreement:123"}, eventstream.Event{Name: "second"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, first.Cursor)
	require.NoError(t, err)
	require.False(t, sub.CursorGap)

	select {
	case delivered := <-sub.Records:
		assert.Equal(t, second.Cursor, delivered.Cursor)
		assert.Equal(t, "second", delivered.Event.Name)
	case <-time.After(time.Second):
		t.Fatal("expected replay record")
	}

	stats := stream.SnapshotStats()
	assert.EqualValues(t, 1, stats.ResumeCount)
}

func TestSubscribeReturnsGapForMissingCursorWhenMatchedReplayExists(t *testing.T) {
	stream := eventstream.New()
	stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "first"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := stream.Subscribe(ctx, eventstream.Scope{"tenant": "t1"}, "missing")
	require.NoError(t, err)
	assert.True(t, sub.CursorGap)
	assert.Equal(t, "cursor_not_found", sub.CursorGapReason)

	_, ok := <-sub.Records
	assert.False(t, ok)

	stats := stream.SnapshotStats()
	assert.EqualValues(t, 1, stats.DropReasons["cursor_not_found"])
}

func TestMatcherHelpers(t *testing.T) {
	subscription := eventstream.Scope{"tenant": "t1"}
	published := eventstream.Scope{
		"tenant":   "t1",
		"resource": "agreement:123",
	}

	assert.False(t, eventstream.ExactMatch(subscription, published))
	assert.True(t, eventstream.SubsetMatch(subscription, published))
	assert.False(t, eventstream.SubsetMatch(subscription, eventstream.Scope{"tenant": "t2"}))
	assert.True(t, eventstream.ExactMatch(
		eventstream.Scope{"tenant": "t1", "resource": "agreement:123"},
		published,
	))
}
