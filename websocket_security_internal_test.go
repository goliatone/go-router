package router

import (
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
)

func TestValidateOriginDefaultsToSameOrigin(t *testing.T) {
	config := DefaultWebSocketConfig()

	ctx := NewMockContext()
	ctx.HeadersM["Origin"] = "https://app.example.com"
	ctx.HeadersM["Host"] = "app.example.com"
	ctx.HeadersM["X-Forwarded-Proto"] = "https"

	if !validateOrigin(ctx, config) {
		t.Fatal("expected same-origin request to be allowed by default")
	}

	ctx.HeadersM["Host"] = "api.example.com"
	if validateOrigin(ctx, config) {
		t.Fatal("expected cross-origin request to be rejected by default")
	}
}

func TestMatchesOriginPatternRejectsWildcardBypass(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		pattern  string
		expected bool
	}{
		{
			name:     "host wildcard matches subdomain",
			origin:   "https://app.example.com",
			pattern:  "*.example.com",
			expected: true,
		},
		{
			name:     "host wildcard rejects apex",
			origin:   "https://example.com",
			pattern:  "*.example.com",
			expected: false,
		},
		{
			name:     "host wildcard rejects suffix spoof",
			origin:   "https://badexample.com",
			pattern:  "*.example.com",
			expected: false,
		},
		{
			name:     "full origin wildcard matches subdomain",
			origin:   "https://app.example.com",
			pattern:  "https://*.example.com",
			expected: true,
		},
		{
			name:     "full origin wildcard rejects spoof",
			origin:   "https://badexample.com",
			pattern:  "https://*.example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesOriginPattern(tt.origin, tt.pattern); got != tt.expected {
				t.Fatalf("matchesOriginPattern(%q, %q) = %v, want %v", tt.origin, tt.pattern, got, tt.expected)
			}
		})
	}
}

func TestHTTPRouterContextHeaderHostUsesRequestHost(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.com/ws", nil)
	ctx := newHTTPRouterContext(httptest.NewRecorder(), req, httprouter.Params{}, nil)

	if got := ctx.Header("Host"); got != "example.com" {
		t.Fatalf("expected host header fallback to request host, got %q", got)
	}
}

func TestIsSameOriginUsesRequestHostOnHTTPRouter(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.com/ws", nil)
	req.Header.Set("Origin", "https://example.com")
	ctx := newHTTPRouterContext(httptest.NewRecorder(), req, httprouter.Params{}, nil)

	if !isSameOrigin(ctx) {
		t.Fatal("expected request host to satisfy same-origin check")
	}

	req.Header.Set("Origin", "https://evil.example.com")
	if isSameOrigin(ctx) {
		t.Fatal("expected mismatched origin to fail same-origin check")
	}
}
