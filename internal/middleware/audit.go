package middleware

import (
	"log/slog"
	"net/http"
)

func Audit(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Persisting audit rows belongs in the audit module once auth/user context exists.
			next.ServeHTTP(w, r)
		})
	}
}
