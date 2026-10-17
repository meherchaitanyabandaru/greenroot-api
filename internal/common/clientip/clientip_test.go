package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolve(t *testing.T) {
	trusted, err := ParsePrefixes([]string{"10.0.0.0/8", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		remote string
		xff    []string
		realIP string
		want   string
	}{
		{"direct client, no headers", "203.0.113.7:51234", nil, "", "203.0.113.7"},
		{"port is stripped so every connection shares one key", "203.0.113.7:60001", nil, "", "203.0.113.7"},
		{"untrusted peer cannot spoof X-Forwarded-For", "203.0.113.7:1", []string{"1.2.3.4"}, "", "203.0.113.7"},
		{"untrusted peer cannot spoof X-Real-IP", "203.0.113.7:1", nil, "1.2.3.4", "203.0.113.7"},
		{"trusted proxy: rightmost untrusted hop wins", "10.0.0.5:1", []string{"6.6.6.6, 198.51.100.9"}, "", "198.51.100.9"},
		{"trusted proxy chain is skipped", "10.0.0.5:1", []string{"198.51.100.9, 10.1.1.1"}, "", "198.51.100.9"},
		{"multiple XFF headers are joined", "10.0.0.5:1", []string{"6.6.6.6", "198.51.100.9"}, "", "198.51.100.9"},
		{"malformed hop falls back to peer", "10.0.0.5:1", []string{"198.51.100.9, not-an-ip"}, "", "10.0.0.5"},
		{"all hops trusted: leftmost", "10.0.0.5:1", []string{"10.2.2.2, 10.3.3.3"}, "", "10.2.2.2"},
		{"trusted proxy X-Real-IP", "127.0.0.1:1", nil, "198.51.100.9", "198.51.100.9"},
		{"IPv6 peer", "[2001:db8::1]:443", nil, "", "2001:db8::1"},
		{"IPv4-mapped IPv6 is unmapped", "[::ffff:203.0.113.7]:1", nil, "", "203.0.113.7"},
		{"unparseable peer", "garbage", nil, "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.realIP != "" {
				r.Header.Set("X-Real-IP", tc.realIP)
			}
			if got := Resolve(r, trusted); got != tc.want {
				t.Fatalf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNoTrustedProxiesIgnoresHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:9"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := Resolve(r, nil); got != "127.0.0.1" {
		t.Fatalf("Resolve = %q, want peer", got)
	}
}

func TestMiddlewareAndFrom(t *testing.T) {
	var got string
	h := Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = From(r) }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:4444"
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != "203.0.113.7" {
		t.Fatalf("From = %q", got)
	}

	// Without the middleware From falls back to the peer.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "203.0.113.8:1"
	if From(r2) != "203.0.113.8" {
		t.Fatalf("fallback From = %q", From(r2))
	}
}

func TestParsePrefixesRejectsGarbage(t *testing.T) {
	if _, err := ParsePrefixes([]string{"10.0.0.0/33"}); err == nil {
		t.Fatal("expected error for bad CIDR")
	}
	if _, err := ParsePrefixes([]string{"nope"}); err == nil {
		t.Fatal("expected error for bad IP")
	}
}
