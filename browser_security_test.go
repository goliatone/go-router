package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateCookie(t *testing.T) {
	valid := []Cookie{
		{SameSite: CookieSameSiteLaxMode},
		{SameSite: CookieSameSiteStrictMode},
		{SameSite: CookieSameSiteDisabled},
		{SameSite: CookieSameSiteNoneMode, Secure: true},
	}
	for _, tc := range valid {
		if err := ValidateCookie(tc); err != nil {
			t.Fatalf("expected cookie %+v to be valid: %v", tc, err)
		}
	}
	if err := ValidateCookie(Cookie{SameSite: "bogus"}); err == nil {
		t.Fatalf("expected invalid SameSite to fail validation")
	}
	if err := ValidateCookie(Cookie{SameSite: CookieSameSiteNoneMode}); err == nil {
		t.Fatalf("expected SameSite=None without Secure to fail validation")
	}
}

func TestSessionCookiePresets(t *testing.T) {
	firstParty := FirstPartySessionCookie("auth", "token")
	if firstParty.Name != "auth" || firstParty.Value != "token" || firstParty.Path != "/" || !firstParty.HTTPOnly || firstParty.SameSite != CookieSameSiteLaxMode || !firstParty.SessionOnly {
		t.Fatalf("unexpected first party cookie preset: %+v", firstParty)
	}

	embedded := EmbeddedThirdPartySessionCookie("auth", "token")
	if embedded.Name != "auth" || embedded.Value != "token" || embedded.Path != "/" || !embedded.HTTPOnly || !embedded.Secure || embedded.SameSite != CookieSameSiteNoneMode || !embedded.SessionOnly {
		t.Fatalf("unexpected embedded cookie preset: %+v", embedded)
	}
}

func TestSanitizeRedirectTarget(t *testing.T) {
	target, ok := SanitizeRedirectTarget("https://example.com/admin?tab=1", "https", "example.com")
	if !ok || target != "/admin?tab=1" {
		t.Fatalf("expected same-origin absolute redirect to be sanitized, got %q ok=%v", target, ok)
	}

	if _, ok := SanitizeRedirectTarget("https://evil.example/admin", "https", "example.com"); ok {
		t.Fatalf("expected foreign origin redirect to be rejected")
	}
}

func TestOriginProtection(t *testing.T) {
	server := NewHTTPServer().(*HTTPServer)
	server.Router().Post("/protected", func(c Context) error {
		return c.SendStatus(http.StatusOK)
	}, OriginProtection())

	sameOriginReq := httptest.NewRequest(http.MethodPost, "http://example.com/protected", nil)
	sameOriginReq.Host = "example.com"
	sameOriginReq.Header.Set("Origin", "http://example.com")
	sameOriginResp := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(sameOriginResp, sameOriginReq)
	if sameOriginResp.Code != http.StatusOK {
		t.Fatalf("expected same-origin POST to pass, got %d", sameOriginResp.Code)
	}

	allowedOriginReq := httptest.NewRequest(http.MethodPost, "http://example.com/protected", nil)
	allowedOriginReq.Host = "example.com"
	allowedOriginReq.Header.Set("Origin", "https://partner.example")
	serverAllowed := NewHTTPServer().(*HTTPServer)
	serverAllowed.Router().Post("/protected", func(c Context) error {
		return c.SendStatus(http.StatusOK)
	}, OriginProtection(OriginProtectionConfig{AllowedOrigins: []string{"https://partner.example"}}))
	allowedOriginResp := httptest.NewRecorder()
	serverAllowed.WrappedRouter().ServeHTTP(allowedOriginResp, allowedOriginReq)
	if allowedOriginResp.Code != http.StatusOK {
		t.Fatalf("expected allowed origin POST to pass, got %d", allowedOriginResp.Code)
	}

	foreignOriginReq := httptest.NewRequest(http.MethodPost, "http://example.com/protected", nil)
	foreignOriginReq.Host = "example.com"
	foreignOriginReq.Header.Set("Origin", "https://evil.example")
	foreignOriginResp := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(foreignOriginResp, foreignOriginReq)
	if foreignOriginResp.Code != http.StatusForbidden {
		t.Fatalf("expected foreign origin POST to fail, got %d", foreignOriginResp.Code)
	}

	refererReq := httptest.NewRequest(http.MethodPost, "http://example.com/protected", nil)
	refererReq.Host = "example.com"
	refererReq.Header.Set("Referer", "https://evil.example/form")
	refererResp := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(refererResp, refererReq)
	if refererResp.Code != http.StatusForbidden {
		t.Fatalf("expected foreign referer POST to fail, got %d", refererResp.Code)
	}

	missingHeadersReq := httptest.NewRequest(http.MethodPost, "http://example.com/protected", nil)
	missingHeadersReq.Host = "example.com"
	missingHeadersResp := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(missingHeadersResp, missingHeadersReq)
	if missingHeadersResp.Code != http.StatusForbidden {
		t.Fatalf("expected unsafe POST without origin headers to fail, got %d", missingHeadersResp.Code)
	}
}
