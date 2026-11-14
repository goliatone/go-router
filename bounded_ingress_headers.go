package router

import (
	"bytes"
	"github.com/valyala/fasthttp"
)

type ingressHeader interface {
	VisitAll(func([]byte, []byte))
	Del(string)
	Set(string, string)
}

// Native header normalization may be disabled. Delete every spelling of the
// reserved field before publishing one canonical value, without changing any
// unrelated field. Copy names before deletion invalidates VisitAll's slices.
func setIngressHeader(header ingressHeader, id string) {
	var names []string
	header.VisitAll(func(key, _ []byte) {
		if bytes.EqualFold(key, []byte(XRequestID)) {
			names = append(names, string(key))
		}
	})
	for _, name := range names {
		header.Del(name)
	}
	header.Set(XRequestID, id)
}

func restoreIngressResponseHeader(c *fasthttp.RequestCtx) {
	if c == nil {
		return
	}
	if life, ok := c.UserValue(ingressContextKey{}).(*ingressLifetime); ok {
		setIngressHeader(&c.Response.Header, life.id)
	}
}
