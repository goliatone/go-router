package router

import (
	"sync"
	"time"
)

func startWebSocketPingLoop(wsCtx WebSocketContext, config WebSocketConfig, logger Logger) func() {
	if wsCtx == nil || !config.keepAliveEnabled() {
		return func() {}
	}

	ticker := time.NewTicker(config.PingPeriod)
	done := make(chan struct{})
	stopped := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer ticker.Stop()
		defer close(stopped)
		for {
			select {
			case <-ticker.C:
				if err := wsCtx.WritePing(nil); err != nil {
					if logger != nil {
						logger.Info("WebSocket ping error", "error", err)
					}
					return
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(done)
			<-stopped
		})
	}
}
