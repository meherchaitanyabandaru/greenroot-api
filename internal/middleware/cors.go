package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

const (
	corsAllowMethods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"
	corsAllowHeaders = "Authorization,Content-Type,Idempotency-Key,If-Match,X-Requested-With,X-Request-Id"
	// Headers browser clients must be able to read: PDF filenames, the
	// subscription grace warning and the request ID for support.
	corsExposeHeaders = "Content-Disposition,X-Subscription-Warning,X-Request-Id"
	corsMaxAge        = "600"
)

// CORS allows browser calls from an exact list of origins. Auth uses bearer
// tokens (no cookies), so credentials are never allowed. Native mobile apps
// send no Origin header and are unaffected.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, raw := range allowedOrigins {
		origin, ok := NormalizeOrigin(raw)
		if !ok {
			slog.Warn("ignoring invalid CORS origin; use scheme://host[:port] with no path", "origin", raw)
			continue
		}
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			header := w.Header()
			// The response differs by origin, so shared caches must key on it.
			header.Add("Vary", "Origin")
			_, ok := allowed[strings.ToLower(origin)]

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				header.Add("Vary", "Access-Control-Request-Method")
				header.Add("Vary", "Access-Control-Request-Headers")
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				header.Set("Access-Control-Allow-Origin", origin)
				header.Set("Access-Control-Allow-Methods", corsAllowMethods)
				header.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				header.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if ok {
				header.Set("Access-Control-Allow-Origin", origin)
				header.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NormalizeOrigin lower-cases and validates an origin ("https://admin.example.com",
// optional port). A trailing slash is tolerated; paths, queries and "*" are not.
func NormalizeOrigin(raw string) (string, bool) {
	s := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}
