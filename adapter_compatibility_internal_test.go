package router

import "github.com/julienschmidt/httprouter"

// NewCompatibilityHTTPServer exposes the existing package-private view setup
// to the external shared-contract fixture without widening the production API.
func NewCompatibilityHTTPServer(views Views) Server[*httprouter.Router] {
	server, ok := NewHTTPServer().(*HTTPServer)
	if !ok {
		panic("unexpected HTTP server type")
	}
	server.views = views
	return server
}
