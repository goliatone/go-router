package requestid

import (
	"context"
	"io"
	"net/http"
	"testing"

	router "github.com/goliatone/go-router"
)

func TestIngressOwnedIdentity(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			var configure router.BoundedIngressServer
			var request func(*http.Request) (*http.Response, error)
			middleware := New(Config{Header: "X-Alternate-ID", Generator: func() string { t.Error("generated second ID"); return "wrong" }})
			handler := func(c router.Context) error {
				id, _ := router.IngressRequestIDFromContext(c.Context())
				if c.Locals("requestid") != id {
					return c.SendStatus(500)
				}
				return c.SendString(id)
			}
			replace := func(next router.HandlerFunc) router.HandlerFunc {
				return func(c router.Context) error { c.SetContext(context.Background()); return next(c) }
			}
			if major == "v2" {
				s := router.NewFiberAdapter()
				configure = s.(router.BoundedIngressServer)
				s.Router().Use(replace, middleware)
				s.Router().Get("/", handler)
				request = func(r *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(r) }
			} else {
				s := router.NewFiberV3Adapter()
				configure = s.(router.BoundedIngressServer)
				s.Router().Use(replace, middleware)
				s.Router().Get("/", handler)
				request = func(r *http.Request) (*http.Response, error) { return s.WrappedRouter().Test(r) }
			}
			if err := configure.ConfigureBoundedIngress(router.BoundedIngressOptions{}); err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest("GET", "http://example.test/", nil)
			req.Header.Set(router.XRequestID, "client")
			req.Header.Set("X-Alternate-ID", "alternate-client")
			res, err := request(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			b, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 200 || string(b) == "" || string(b) == "client" || res.Header.Get(router.XRequestID) != string(b) || res.Header.Get("X-Alternate-ID") != string(b) {
				t.Fatal(res.StatusCode, res.Header, string(b))
			}
		})
	}
}
