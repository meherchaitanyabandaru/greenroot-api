// Package clientip resolves the real client IP address of a request.
//
// Proxy headers (X-Forwarded-For, X-Real-IP) are set by whoever sends the
// request, so they are trusted only when the TCP peer is a configured trusted
// proxy (TRUSTED_PROXIES). Otherwise the peer address itself is the client.
// Everything that needs a client IP — rate limits, audit logs, sessions —
// must call From instead of reading r.RemoteAddr or headers directly.
package clientip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type ctxKey struct{}

// ParsePrefixes parses a list of CIDRs or bare IPs ("10.0.0.0/8", "127.0.0.1").
func ParsePrefixes(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.Contains(v, "/") {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q: %w", v, err)
			}
			prefixes = append(prefixes, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", v, err)
		}
		a = a.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	return prefixes, nil
}

// Middleware resolves the client IP once per request and stores it in the
// request context. It does not modify r.RemoteAddr.
func Middleware(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := Resolve(r, trusted)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ip)))
		})
	}
}

// From returns the client IP resolved by Middleware. Without the middleware
// (for example in unit tests) it falls back to the TCP peer address.
func From(r *http.Request) string {
	if ip, ok := r.Context().Value(ctxKey{}).(string); ok {
		return ip
	}
	return peer(r).String()
}

// Resolve returns the client IP for r. The peer address is used unless it is
// a trusted proxy; then X-Forwarded-For is walked from right to left and the
// first address that is not a trusted proxy is the client. X-Real-IP is used
// only when a trusted proxy sent it and X-Forwarded-For is absent.
func Resolve(r *http.Request, trusted []netip.Prefix) string {
	p := peer(r)
	if !p.IsValid() {
		return "unknown"
	}
	if !isTrusted(p, trusted) {
		return p.String()
	}

	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				// A malformed hop means the chain beyond it cannot be trusted.
				return p.String()
			}
			a = a.Unmap()
			if !isTrusted(a, trusted) {
				return a.String()
			}
		}
		// Every hop is a trusted proxy: the leftmost is the closest we know.
		if a, err := netip.ParseAddr(strings.TrimSpace(hops[0])); err == nil {
			return a.Unmap().String()
		}
		return p.String()
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if a, err := netip.ParseAddr(xri); err == nil {
			return a.Unmap().String()
		}
	}
	return p.String()
}

func peer(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
