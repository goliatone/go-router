package router_test

import (
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// TestHubConcurrentAccess verifies the fix for Bug #4
// This test exercises the hub's concurrent operations to ensure no race conditions
func TestHubConcurrentAccess(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	var wg sync.WaitGroup
	numWorkers := 20
	operations := 50

	// Workers that perform concurrent operations on the hub
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operations; j++ {
				// Mix of operations that access the clients map
				switch j % 4 {
				case 0:
					// Read operations (should be protected by RLock)
					hub.ClientCount()
				case 1:
					// Read operations (should be protected by RLock)
					clients := hub.Clients()
					_ = len(clients)
				case 2:
					// Broadcast operations (reads clients map internally)
					hub.Broadcast([]byte("test message"))
				case 3:
					// JSON broadcast operations (reads clients map internally)
					hub.BroadcastJSON(map[string]interface{}{
						"worker": workerID,
						"op":     j,
					})
				}

				// Small delay to increase contention
				if j%10 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify hub is still functional
	finalCount := hub.ClientCount()
	if finalCount < 0 {
		t.Errorf("Invalid final client count: %d", finalCount)
	}

	t.Logf("Test completed successfully. Hub processed %d workers with %d operations each",
		numWorkers, operations)
}

// TestHubStressOperations tests the hub under stress to verify race condition fixes
func TestHubStressOperations(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	var wg sync.WaitGroup
	duration := 100 * time.Millisecond

	// Multiple concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			start := time.Now()
			opCount := 0

			for time.Since(start) < duration {
				// These operations read the clients map and should be race-free
				hub.ClientCount()
				hub.Clients()
				hub.Broadcast([]byte("stress test"))

				opCount++
				if opCount%50 == 0 {
					time.Sleep(time.Microsecond)
				}
			}

			t.Logf("Reader %d completed %d operations", readerID, opCount)
		}(i)
	}

	wg.Wait()

	// Hub should still be functional after stress test
	if count := hub.ClientCount(); count < 0 {
		t.Errorf("Hub corrupted after stress test: count=%d", count)
	}
}
