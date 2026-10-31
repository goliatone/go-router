package router

import (
	"sync"
	"time"
)

// periodicCleanup owns a serial cleanup loop and joins it when closed.
type periodicCleanup struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newPeriodicCleanup(interval time.Duration, sweep func()) *periodicCleanup {
	c := &periodicCleanup{stop: make(chan struct{}), done: make(chan struct{})}
	go c.run(interval, sweep)
	return c
}

func (c *periodicCleanup) run(interval time.Duration, sweep func()) {
	defer close(c.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// A queued tick must not keep the loop alive after an active sweep exits.
		select {
		case <-c.stop:
			return
		default:
		}
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			sweep()
		}
	}
}

func (c *periodicCleanup) close() {
	if c == nil {
		return
	}
	c.once.Do(func() { close(c.stop) })
	<-c.done
}
