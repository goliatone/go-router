package router_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	p6 "github.com/flosch/pongo2/v6"
	p7 "github.com/flosch/pongo2/v7"

	router "github.com/goliatone/go-router"
)

func TestFiberV3ViewFilterResultBoundary(t *testing.T) {
	for _, reload := range []bool{false, true} {
		for _, tc := range []struct {
			name            string
			value           *p7.Value
			failure         error
			want, wantError string
		}{
			{name: "foreign-unsafe", value: p7.AsValue(p6.AsValue("<script>bad</script>")), wantError: "migrate helpers/data"},
			{name: "foreign-safe", value: p7.AsSafeValue(p6.AsSafeValue("<script>bad</script>")), wantError: "migrate helpers/data"},
			{name: "foreign-false", value: p7.AsValue(p6.AsValue(false)), wantError: "migrate helpers/data"},
			{name: "native-unsafe", value: p7.AsValue("<b>unsafe</b>"), want: "&lt;b&gt;unsafe&lt;/b&gt;"},
			{name: "native-safe", value: p7.AsSafeValue("<b>safe</b>"), want: "<b>safe</b>"},
			{name: "native-false", value: p7.AsValue(false), want: "False"},
			{name: "error", failure: errors.New("filter failed"), wantError: "filter failed"},
		} {
			t.Run(tc.name+"/reload="+map[bool]string{false: "false", true: "true"}[reload], func(t *testing.T) {
				views := newContractViews(t, `{{ input|result_boundary }}`, nil, reload)
				err := requireTestType[router.FiberV3ViewExtensions](t, views).RegisterFilter("result_boundary", func(_, _ *p7.Value) (*p7.Value, error) { return tc.value, tc.failure })
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					var out bytes.Buffer
					err := views.Render(&out, "page", map[string]any{"input": "input"})
					if tc.wantError != "" {
						if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(out.String(), "<script>") {
							t.Fatalf("output=%q err=%v", out.String(), err)
						}
					} else if err != nil || out.String() != tc.want {
						t.Fatalf("output=%q want=%q err=%v", out.String(), tc.want, err)
					}
				}
			})
		}
	}
}
