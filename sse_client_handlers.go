package router

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

// Embed SSE client files at compile time.
//
//go:embed sseclient/dist/client.js
var sseClientJS []byte

//go:embed sseclient/dist/client.js.map
var sseClientJSMap []byte

//go:embed sseclient/dist/client.min.js
var sseClientMinJS []byte

//go:embed sseclient/dist/client.mjs
var sseClientModuleJS []byte

//go:embed sseclient/dist/client.d.ts
var sseClientDTS []byte

const (
	SSEClientVersion = WebSocketClientVersion
	SSEClientBuild   = WebSocketClientBuild
)

var (
	sseClientETag       = generateETag(sseClientJS, "sse.js")
	sseClientJSMapETag  = generateETag(sseClientJSMap, "sse.js.map")
	sseClientMinETag    = generateETag(sseClientMinJS, "sse.min.js")
	sseClientModuleETag = generateETag(sseClientModuleJS, "sse.mjs")
	sseClientDTSETag    = generateETag(sseClientDTS, "sse.d.ts")
)

type sseEmbeddedAsset struct {
	content     []byte
	contentType string
	etag        string
	filename    string
	maxAge      time.Duration
}

func setSSEClientHeaders(c Context, contentType, etag string, maxAge time.Duration) {
	c.SetHeader("Content-Type", contentType)
	c.SetHeader("Cache-Control", fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds())))
	c.SetHeader("ETag", etag)
	c.SetHeader("Vary", "Accept-Encoding")
	c.SetHeader("X-SSE-Client-Version", SSEClientVersion)
	c.SetHeader("X-SSE-Client-Build", SSEClientBuild)
}

func serveSSEEmbeddedAsset(asset sseEmbeddedAsset) HandlerFunc {
	return func(c Context) error {
		if checkETag(c, asset.etag) {
			return c.Status(304).Send(nil)
		}

		setSSEClientHeaders(c, asset.contentType, asset.etag, asset.maxAge)
		c.SetHeader("Content-Disposition", fmt.Sprintf("inline; filename=%s", asset.filename))

		if c.Query("cors") == "true" {
			c.SetHeader("Access-Control-Allow-Origin", "*")
			c.SetHeader("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Accept, Accept-Encoding, If-None-Match")
		}

		return c.Status(200).Send(asset.content)
	}
}

// SSEClientHandler serves the browser-targeted SSE client bundle.
// When minified is true it serves the minified IIFE build, otherwise it serves
// the development IIFE build with sourcemap references.
func SSEClientHandler(minified bool) HandlerFunc {
	if minified {
		return serveSSEEmbeddedAsset(sseEmbeddedAsset{
			content:     sseClientMinJS,
			contentType: "application/javascript; charset=utf-8",
			etag:        sseClientMinETag,
			filename:    "client.min.js",
			maxAge:      time.Hour,
		})
	}

	return serveSSEEmbeddedAsset(sseEmbeddedAsset{
		content:     sseClientJS,
		contentType: "application/javascript; charset=utf-8",
		etag:        sseClientETag,
		filename:    "client.js",
		maxAge:      time.Hour,
	})
}

// SSEClientModuleHandler serves the ESM build for module-aware applications.
func SSEClientModuleHandler() HandlerFunc {
	return serveSSEEmbeddedAsset(sseEmbeddedAsset{
		content:     sseClientModuleJS,
		contentType: "application/javascript; charset=utf-8",
		etag:        sseClientModuleETag,
		filename:    "client.mjs",
		maxAge:      time.Hour,
	})
}

// SSEClientSourceMapHandler serves the source map for the non-minified IIFE build.
func SSEClientSourceMapHandler() HandlerFunc {
	return serveSSEEmbeddedAsset(sseEmbeddedAsset{
		content:     sseClientJSMap,
		contentType: "application/json; charset=utf-8",
		etag:        sseClientJSMapETag,
		filename:    "client.js.map",
		maxAge:      30 * time.Minute,
	})
}

// SSEClientTypesHandler serves TypeScript declarations for the embedded client.
func SSEClientTypesHandler() HandlerFunc {
	return serveSSEEmbeddedAsset(sseEmbeddedAsset{
		content:     sseClientDTS,
		contentType: "text/plain; charset=utf-8",
		etag:        sseClientDTSETag,
		filename:    "client.d.ts",
		maxAge:      time.Hour,
	})
}

type SSEClientHandlerConfig struct {
	BaseRoute string
}

func normalizeSSEClientBaseRoute(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/sseclient"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/sseclient"
	}
	return path
}

// RegisterSSEHandlers registers the embedded SSE client assets on the router.
func RegisterSSEHandlers[T any](app Router[T], cfgs ...SSEClientHandlerConfig) {
	cfg := SSEClientHandlerConfig{
		BaseRoute: "/sseclient",
	}

	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	base := normalizeSSEClientBaseRoute(cfg.BaseRoute)
	app.Get(base+"/client.js", SSEClientHandler(false))
	app.Get(base+"/client.js.map", SSEClientSourceMapHandler())
	app.Get(base+"/client.min.js", SSEClientHandler(true))
	app.Get(base+"/client.mjs", SSEClientModuleHandler())
	app.Get(base+"/client.d.ts", SSEClientTypesHandler())
	app.Get(base+"/info", SSEClientInfoHandler())
	app.Get(base+"/", func(c Context) error {
		return c.Redirect(base+"/info", 302)
	})
}

// SSEClientInfo returns metadata for the embedded SSE client assets.
func SSEClientInfo() map[string]any {
	return map[string]any{
		"version": SSEClientVersion,
		"build":   SSEClientBuild,
		"files": map[string]any{
			"client.js": map[string]any{
				"size": len(sseClientJS),
				"etag": sseClientETag,
			},
			"client.js.map": map[string]any{
				"size": len(sseClientJSMap),
				"etag": sseClientJSMapETag,
			},
			"client.min.js": map[string]any{
				"size": len(sseClientMinJS),
				"etag": sseClientMinETag,
			},
			"client.mjs": map[string]any{
				"size": len(sseClientModuleJS),
				"etag": sseClientModuleETag,
			},
			"client.d.ts": map[string]any{
				"size": len(sseClientDTS),
				"etag": sseClientDTSETag,
			},
		},
		"compression": map[string]any{
			"original_size": len(sseClientJS),
			"minified_size": len(sseClientMinJS),
			"compression_ratio": fmt.Sprintf("%.1f%%",
				(1.0-float64(len(sseClientMinJS))/float64(len(sseClientJS)))*100),
		},
	}
}

// SSEClientInfoHandler serves embedded SSE client metadata as JSON.
func SSEClientInfoHandler() HandlerFunc {
	return func(c Context) error {
		c.SetHeader("Content-Type", "application/json; charset=utf-8")
		c.SetHeader("Cache-Control", "public, max-age=300")
		return c.JSON(200, SSEClientInfo())
	}
}

// RegisterSSEClientRoutesManual provides named handlers for manual registration.
func RegisterSSEClientRoutesManual() map[string]HandlerFunc {
	return map[string]HandlerFunc{
		"client.js":     SSEClientHandler(false),
		"client.js.map": SSEClientSourceMapHandler(),
		"client.min.js": SSEClientHandler(true),
		"client.mjs":    SSEClientModuleHandler(),
		"client.d.ts":   SSEClientTypesHandler(),
		"info":          SSEClientInfoHandler(),
		"":              func(c Context) error { return c.Redirect("/sseclient/info", 302) },
	}
}

// SSEClientCORSMiddleware handles OPTIONS requests for embedded SSE client assets.
func SSEClientCORSMiddleware() HandlerFunc {
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
