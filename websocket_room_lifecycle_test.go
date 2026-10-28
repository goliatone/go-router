package router

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Run deadlock regressions in a subprocess so a failing implementation cannot
// strand locked rooms or workers in the rest of the suite.
func TestRoomLifecycleCleanup(t *testing.T) {
	if mode := os.Getenv("ROUTER_ROOM_CLEANUP_CASE"); mode != "" {
		hub := NewWSHub()
		defer hub.Close()
		room, err := hub.CreateRoom(context.Background(), "cleanup", "Cleanup", RoomConfig{DestroyWhenEmpty: true})
		if err != nil {
			t.Fatal(err)
		}
		hookDone := make(chan struct{}, 1)
		var hooks atomic.Int32
		room.OnDestroy(func(context.Context, *Room) error {
			hooks.Add(1)
			hookDone <- struct{}{}
			return nil
		})
		switch mode {
		case "room":
			if err := room.Destroy(); err != nil {
				t.Fatal(err)
			}
			if err := room.Destroy(); err == nil {
				t.Fatal("repeated destruction must retain its error")
			}
		case "manager":
			if err := hub.roomManager.RemoveRoom(room.ID()); err != nil {
				t.Fatal(err)
			}
		case "concurrent":
			var wg sync.WaitGroup
			for i := range 16 {
				wg.Go(func() {
					if i%2 == 0 {
						_ = room.Destroy()
					} else {
						_ = hub.roomManager.RemoveRoom(room.ID())
					}
				})
			}
			wg.Wait()
		case "reentrant":
			var replacement *Room
			client := &roomLifecycleClient{send: func() error {
				if !room.IsDestroyed() || room.ClientCount() != 0 {
					t.Error("notification observed a live room or stale membership")
				}
				replacement, err = hub.CreateRoom(context.Background(), room.ID(), "Replacement", RoomConfig{})
				if err != nil {
					return err
				}
				return nil
			}}
			if err := room.AddClient(context.Background(), client); err != nil {
				t.Fatal(err)
			}
			if err := hub.roomManager.RemoveRoom(room.ID()); err != nil {
				t.Fatal(err)
			}
			// Removal resumed after the notification created a replacement.
			found, err := hub.GetRoom(room.ID())
			if err != nil || found != replacement {
				t.Fatalf("old cleanup removed replacement: %v", err)
			}
			if err := replacement.Destroy(); err != nil {
				t.Fatal(err)
			}
		case "joining":
			room.config.Private = true
			entered, release := make(chan struct{}), make(chan struct{})
			room.SetAuthFunc(func(context.Context, *Room, WSClient) (bool, error) {
				close(entered)
				<-release
				return true, nil
			})
			joined := make(chan error, 1)
			go func() { joined <- room.AddClient(context.Background(), &roomLifecycleClient{}) }()
			<-entered
			if err := room.Destroy(); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-joined; err == nil || room.ClientCount() != 0 {
				t.Fatal("authorization resumed into a destroyed room")
			}
		}
		if !room.IsDestroyed() || len(hub.ListRooms()) != 0 {
			t.Fatal("room remains live or registered after cleanup")
		}
		select {
		case <-room.ctx.Done():
		default:
			t.Fatal("room worker context was not canceled")
		}
		select {
		case <-room.watchDone:
		default:
			t.Fatal("cleanup returned before the empty-room watcher stopped")
		}
		<-hookDone
		if hooks.Load() != 1 {
			t.Fatal("destruction hook did not execute exactly once")
		}
		return
	}
	for _, mode := range []string{"room", "manager", "concurrent", "reentrant", "joining"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRoomLifecycleCleanup$")
			cmd.Env = append(os.Environ(), "ROUTER_ROOM_CLEANUP_CASE="+mode)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("cleanup %s did not complete: %v (deadline: %v)\n%s", mode, err, ctx.Err(), out)
			}
		})
	}
}

type roomLifecycleClient struct {
	WSClient
	send func() error
}

func (*roomLifecycleClient) ID() string { return "cleanup-client" }

func (c *roomLifecycleClient) SendJSON(any) error {
	if c.send != nil {
		return c.send()
	}
	return nil
}
