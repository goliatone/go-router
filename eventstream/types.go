package eventstream

import (
	"context"
	"encoding/json"
	"time"
)

// Scope identifies the published or subscribed audience for a record.
type Scope map[string]string

// MatchFunc decides whether a published scope should be delivered to a
// subscription scope.
type MatchFunc func(subscription Scope, published Scope) bool

// Event is the transport-neutral payload stored by the stream core.
type Event struct {
	Name      string            `json:"name"`
	Payload   json.RawMessage   `json:"payload,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Record is a published event with stream-assigned metadata.
type Record struct {
	Cursor      string    `json:"cursor"`
	ScopeKey    string    `json:"scope_key"`
	Event       Event     `json:"event"`
	PublishedAt time.Time `json:"published_at"`
}

// Subscription is a stream registration that yields ordered records.
type Subscription struct {
	Scope           Scope
	ScopeKey        string
	Records         <-chan Record
	CursorGap       bool
	CursorGapReason string
}

// Stats exposes a point-in-time stream snapshot for observability.
type Stats struct {
	PublishedCount    int64
	ResumeCount       int64
	ActiveSubscribers int
	BufferedRecords   int
	DropReasons       map[string]int64
}

// Hooks provides optional observability callbacks for stream lifecycle events.
type Hooks struct {
	OnPublish   func(PublishEvent)
	OnSubscribe func(SubscribeEvent)
	OnDrop      func(DropEvent)
}

// PublishEvent describes a successful publish operation.
type PublishEvent struct {
	Scope    Scope
	ScopeKey string
	Record   Record
}

// SubscribeEvent describes a subscription attempt and its replay state.
type SubscribeEvent struct {
	Scope           Scope
	ScopeKey        string
	AfterCursor     string
	ReplayCount     int
	CursorGap       bool
	CursorGapReason string
}

// DropEvent describes why a subscriber was removed.
type DropEvent struct {
	Scope    Scope
	ScopeKey string
	Reason   string
}

// Stream is the public replayable event stream surface shared by transports.
type Stream interface {
	Publish(scope Scope, event Event) Record
	Subscribe(ctx context.Context, scope Scope, afterCursor string) (*Subscription, error)
	SnapshotStats() Stats
}
