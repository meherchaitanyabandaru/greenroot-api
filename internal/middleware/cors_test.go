package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsServer() http.Handler {
	return CORS([]string{"https://admin.greenroot.in/", "http://localhost:5173", "https://bad.example.com/path", "*"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
}

func do(h http.Handler, method, origin string, preflight bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/orders", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if preflight {
		req.Header.Set("Access-Control-Request-Method", "PUT")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORS_AllowedOrigin(t *testing.T) {
	rec := do(corsServer(), http.MethodGet, "https://admin.greenroot.in", false)
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "https://admin.greenroot.in" {
		t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "Content-Disposition") {
		t.Fatal("Content-Disposition must be exposed so the admin can name PDF downloads")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("bearer-token API must not allow credentials")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", rec.Header().Get("Vary"))
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	rec := do(corsServer(), http.MethodGet, "https://evil.example.com", false)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("headers = %v", rec.Header())
	}
	// Invalid config entries never match.
	if do(corsServer(), http.MethodGet, "https://bad.example.com", false).Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("origin configured with a path must be ignored")
	}
	if do(corsServer(), http.MethodGet, "https://anything.example.com", false).Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("\"*\" must not act as a wildcard")
	}
}

func TestCORS_Preflight(t *testing.T) {
	rec := do(corsServer(), http.MethodOptions, "http://localhost:5173", true)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Max-Age") == "" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
	}
	if rec := do(corsServer(), http.MethodOptions, "https://evil.example.com", true); rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed preflight code = %d", rec.Code)
	}
	// A non-preflight OPTIONS reaches the router (405 / handler), not a blanket 204.
	if rec := do(corsServer(), http.MethodOptions, "", false); rec.Code != http.StatusTeapot {
		t.Fatalf("plain OPTIONS code = %d", rec.Code)
	}
}

func TestCORS_NoOriginUntouched(t *testing.T) {
	rec := do(corsServer(), http.MethodGet, "", false)
	if rec.Header().Get("Vary") != "" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("native app requests must be untouched: %v", rec.Header())
	}
}

func TestNormalizeOrigin(t *testing.T) {
	for in, want := range map[string]string{"https://Admin.GreenRoot.in/": "https://admin.greenroot.in", "http://localhost:5173": "http://localhost:5173"} {
		if got, ok := NormalizeOrigin(in); !ok || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"*", "admin.greenroot.in", "ftp://x.in", "https://x.in/app", "https://x.in?a=1", "https://u:p@x.in"} {
		if _, ok := NormalizeOrigin(bad); ok {
			t.Errorf("NormalizeOrigin(%q) should fail", bad)
		}
	}
}
