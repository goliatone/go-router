package router

import (
	"crypto/md5"
	_ "embed"
	"fmt"
	"time"
)

// Embed WebSocket client files at compile time
//
//go:embed client/client.js
var websocketClientJS []byte

//go:embed client/client.js.map
var websocketClientMinMap []byte

//go:embed client/client.min.js
var websocketClientMinJS []byte

//go:embed client/client.d.ts
var websocketClientDTS []byte

//go:embed client/examples.js
var websocketExamplesJS []byte

//go:embed client/test-client.html
var websocketTestHTML []byte

// Version and build information
const (
	WebSocketClientVersion = "1.0.4"
	WebSocketClientBuild   = "production"
)

// Generate ETags for cache validation
var (
	websocketClientETag       = generateETag(websocketClientJS, "js")
	websocketClientMinETag    = generateETag(websocketClientMinJS, "min.js")
	websocketClientMinMapEtag = generateETag(websocketClientMinMap, "js.map")
	websocketClientDTSETag    = generateETag(websocketClientDTS, "dts")
	websocketExamplesETag     = generateETag(websocketExamplesJS, "examples.js")
	websocketTestETag         = generateETag(websocketTestHTML, "test.html")
)

// generateETag creates a simple ETag from content hash and type
func generateETag(content []byte, fileType string) string {
	hash := md5.Sum(content)
	return fmt.Sprintf(`"ws-%s-%s-%x"`, WebSocketClientVersion, fileType, hash[:8])
}

// setCommonHeaders sets standard headers for all WebSocket client assets
func setCommonHeaders(c Context, contentType, etag string, maxAge time.Duration) {
	c.SetHeader("Content-Type", contentType)
	c.SetHeader("Cache-Control", fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds())))
	c.SetHeader("ETag", etag)
	c.SetHeader("Vary", "Accept-Encoding")
	c.SetHeader("X-WebSocket-Client-Version", WebSocketClientVersion)
	c.SetHeader("X-WebSocket-Client-Build", WebSocketClientBuild)
}

// checkETag checks if the client's ETag matches and returns 304 if unchanged
func checkETag(c Context, etag string) bool {
	clientETag := c.Get("If-None-Match", "")
	return clientETag == etag
}

// WebSocketClientHandler serves the main WebSocket client library
// @param minified bool - serve minified version if true
func WebSocketClientHandler(minified bool) HandlerFunc {
	return func(c Context) error {
		var content []byte
		var etag string
		var filename string

		if minified {
			content = websocketClientMinJS
			etag = websocketClientMinETag
			filename = "client.min.js"
		} else {
			content = websocketClientJS
			etag = websocketClientETag
			filename = "client.js"
		}

		// Check ETag for caching
		if checkETag(c, etag) {
			return c.Status(304).Send(nil)
		}

		// Set headers
		setCommonHeaders(c, "application/javascript; charset=utf-8", etag, time.Hour)
		c.SetHeader("Content-Disposition", fmt.Sprintf("inline; filename=%s", filename))

		// Enable CORS for CDN usage if requested
		if c.Query("cors") == "true" {
			c.SetHeader("Access-Control-Allow-Origin", "*")
			c.SetHeader("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Accept, Accept-Encoding")
		}

		return c.Status(200).Send(content)
	}
}

// WebSocketClientTypesHandler serves TypeScript definitions
func WebSocketClientTypesHandler() HandlerFunc {
	return func(c Context) error {
		// Check ETag for caching
		if checkETag(c, websocketClientDTSETag) {
			return c.Status(304).Send(nil)
		}

		// Set headers
		setCommonHeaders(c, "text/plain; charset=utf-8", websocketClientDTSETag, time.Hour)
		c.SetHeader("Content-Disposition", "inline; filename=client.d.ts")

		// Enable CORS for CDN usage if requested
		if c.Query("cors") == "true" {
			c.SetHeader("Access-Control-Allow-Origin", "*")
			c.SetHeader("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		}

		return c.Status(200).Send(websocketClientDTS)
	}
}

// WebSocketExamplesHandler serves usage examples
func WebsocketClientMinMapHandler() HandlerFunc {
	return func(c Context) error {
		// Check ETag for caching
		if checkETag(c, websocketClientMinMapEtag) {
			return c.Status(304).Send(nil)
		}

		// Set headers (shorter cache for examples)
		setCommonHeaders(c, "application/json; charset=utf-8", websocketClientMinMapEtag, 30*time.Minute)
		c.SetHeader("Content-Disposition", "inline; filename=client.js.map")

		return c.Status(200).Send(websocketClientMinMap)
	}
}

// WebSocketExamplesHandler serves usage examples
func WebSocketExamplesHandler() HandlerFunc {
	return func(c Context) error {
		// Check ETag for caching
		if checkETag(c, websocketExamplesETag) {
			return c.Status(304).Send(nil)
		}

		// Set headers (shorter cache for examples)
		setCommonHeaders(c, "application/javascript; charset=utf-8", websocketExamplesETag, 30*time.Minute)
		c.SetHeader("Content-Disposition", "inline; filename=websocket-examples.js")

		return c.Status(200).Send(websocketExamplesJS)
	}
}

// WebSocketTestHandler serves the interactive test page
func WebSocketTestHandler() HandlerFunc {
	return func(c Context) error {
		// No caching for test page (for development)
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		c.SetHeader("Cache-Control", "no-cache, no-store, must-revalidate")
		c.SetHeader("Pragma", "no-cache")
		c.SetHeader("Expires", "0")
		c.SetHeader("X-WebSocket-Client-Version", WebSocketClientVersion)

		return c.Status(200).Send(websocketTestHTML)
	}
}

type WSClientHandlerConfig struct {
	BaseRoute string
	Minified  bool
	Debug     bool
}

// Convenience handler functions for common patterns
func RegisterWSHandlers[T any](app Router[T], cfgs ...WSClientHandlerConfig) {
	cfg := WSClientHandlerConfig{
		BaseRoute: "/client",
		Minified:  true,
		Debug:     false,
	}

	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Minified {
		app.Get("/client/client.min.js", WebSocketClientHandler(true))
		app.Get("/client/client.js.map", WebsocketClientMinMapHandler())
	} else {
		app.Get("/client/client.js", WebSocketClientHandler(false))
		// Serve source map even when using the non-minified build to keep devtools quiet
		app.Get("/client/client.js.map", WebsocketClientMinMapHandler())
	}

	app.Get("/client/client.d.ts", WSClientTypesHandler())

	if cfg.Debug {
		app.Get("/client/examples.js", WSExamplesHandler())
		app.Get("/client/test", WSTestHandler())
		app.Get("/client/info", WebSocketClientInfoHandler())
	}

	app.Get("/client/", func(c Context) error {
		return c.Redirect("/client/test", 302)
	})
}

// WSClientHandler serves the regular (non-minified) WebSocket client
func WSClientHandler() HandlerFunc {
	return WebSocketClientHandler(false)
}

// WSClientMinHandler serves the minified WebSocket client
func WSClientMinHandler() HandlerFunc {
	return WebSocketClientHandler(true)
}

// WSClientTypesHandler serves TypeScript definitions
func WSClientTypesHandler() HandlerFunc {
	return WebSocketClientTypesHandler()
}

// WSExamplesHandler serves usage examples
func WSExamplesHandler() HandlerFunc {
	return WebSocketExamplesHandler()
}

// WSTestHandler serves the interactive test page
func WSTestHandler() HandlerFunc {
	return WebSocketTestHandler()
}

// WebSocketClientInfo returns information about the embedded client
func WebSocketClientInfo() map[string]any {
	return map[string]any{
		"version": WebSocketClientVersion,
		"build":   WebSocketClientBuild,
		"files": map[string]any{
			"client.js": map[string]any{
				"size": len(websocketClientJS),
				"etag": websocketClientETag,
			},
			"client.min.js": map[string]any{
				"size": len(websocketClientMinJS),
				"etag": websocketClientMinETag,
			},
			"client.d.ts": map[string]any{
				"size": len(websocketClientDTS),
				"etag": websocketClientDTSETag,
			},
			"examples.js": map[string]any{
				"size": len(websocketExamplesJS),
				"etag": websocketExamplesETag,
			},
			"test.html": map[string]any{
				"size": len(websocketTestHTML),
				"etag": websocketTestETag,
			},
		},
		"compression": map[string]any{
			"original_size": len(websocketClientJS),
			"minified_size": len(websocketClientMinJS),
			"compression_ratio": fmt.Sprintf("%.1f%%",
				(1.0-float64(len(websocketClientMinJS))/float64(len(websocketClientJS)))*100),
		},
	}
}

// WebSocketClientInfoHandler serves client information as JSON
func WebSocketClientInfoHandler() HandlerFunc {
	return func(c Context) error {
		info := WebSocketClientInfo()

		c.SetHeader("Content-Type", "application/json; charset=utf-8")
		c.SetHeader("Cache-Control", "public, max-age=300") // 5 minutes

		return c.JSON(200, info)
	}
}

// Note: Due to Go's type system limitations with generics, use manual registration
// Example usage:
//   app.Router().Get("/client/client.js", router.WSClientHandler())
//   app.Router().Get("/client/client.min.js", router.WSClientMinHandler())
//   app.Router().Get("/client/client.d.ts", router.WSClientTypesHandler())
//   app.Router().Get("/client/examples.js", router.WSExamplesHandler())
//   app.Router().Get("/client/test", router.WSTestHandler())
//   app.Router().Get("/client/info", router.WebSocketClientInfoHandler())

// RegisterWebSocketClientRoutesManual provides a helper for manual registration
func RegisterWebSocketClientRoutesManual() map[string]HandlerFunc {
	return map[string]HandlerFunc{
		"client.js":     WSClientHandler(),
		"client.min.js": WSClientMinHandler(),
		"client.d.ts":   WSClientTypesHandler(),
		"examples.js":   WSExamplesHandler(),
		"test":          WSTestHandler(),
		"info":          WebSocketClientInfoHandler(),
		"":              func(c Context) error { return c.Redirect("/client/test", 302) },
	}
}

// Options middleware for CORS preflight requests
func WebSocketClientCORSMiddleware() HandlerFunc {
	return func(c Context) error {
		if c.Method() == "OPTIONS" {
			c.SetHeader("Access-Control-Allow-Origin", "*")
			c.SetHeader("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Accept, Accept-Encoding, If-None-Match")
			c.SetHeader("Access-Control-Max-Age", "86400")
			return c.Status(204).Send(nil)
		}
		return c.Next()
	}
}
