package router

import (
	"testing"
	"time"
)

func TestDeadlineManagerDerivesPingPeriodFromExplicitPongWait(t *testing.T) {
	manager := NewDeadlineManager(nil, WebSocketConfig{
		PongWait: 200 * time.Millisecond,
	})

	want := 180 * time.Millisecond
	if manager.pingPeriod != want {
		t.Fatalf("expected derived ping period %v, got %v", want, manager.pingPeriod)
	}
}
