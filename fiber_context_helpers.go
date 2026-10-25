package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/valyala/fasthttp"
)

func renderFiberViews(w io.Writer, views Views, defaultLayout, name string, layouts []string, merge func() (map[string]any, error)) error {
	if views == nil {
		return fmt.Errorf("render: no template engine registered")
	}
	merged, err := merge()
	if err != nil {
		return err
	}
	if len(layouts) == 0 && defaultLayout != "" {
		layouts = []string{defaultLayout}
	}
	return views.Render(w, name, merged, layouts...)
}

func mergeFiberViewData(bind any, logger Logger, strategy RenderMergeStrategy, visit func(func([]byte, any))) (map[string]any, error) {
	data, err := SerializeAsContext(bind)
	if err != nil {
		return nil, fmt.Errorf("render: error serializing vars: %w", err)
	}
	if data == nil {
		data = map[string]any{}
	}
	if visit == nil {
		return data, nil
	}
	visit(func(key []byte, value any) {
		name := string(key)
		if existing, ok := data[name]; ok {
			if resolved, set := strategy(name, existing, value, logger); set {
				data[name] = resolved
			}
			return
		}
		data[name] = value
	})
	return data, nil
}

func fiberQueryValues(args *fasthttp.Args, name string, meta func() *fiberRequestMeta) []string {
	if args != nil {
		values := []string{}
		for key, value := range args.All() {
			if string(key) == name {
				values = append(values, string(value))
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	snapshot := meta()
	if snapshot == nil {
		return []string{}
	}
	if values, ok := snapshot.queriesMulti[name]; ok {
		return slices.Clone(values)
	}
	if value, ok := snapshot.queries[name]; ok {
		return []string{value}
	}
	return []string{}
}

func prepareFiberStream(c Context, native *fasthttp.RequestCtx, contentType string, r io.Reader, opts ...StreamOption) error {
	options := ResolveStreamOptions(opts...)
	if contentType == "" {
		contentType = defaultDownloadContentType
	}
	applyDownloadHeaders(c, contentType, options)
	if native == nil {
		return errors.New("context unavailable")
	}
	size := -1
	maxInt := int64(^uint(0) >> 1)
	if options.ContentLength > 0 && options.ContentLength <= maxInt {
		size = int(options.ContentLength)
	}
	native.SetBodyStream(r, size)
	native.SetStatusCode(http.StatusOK)
	return nil
}
