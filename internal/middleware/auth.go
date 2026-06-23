package middleware

import (
	"net/http"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

func Auth(jwtService *jwtplatform.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				response.Error(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
				return
			}

			token := strings.TrimPrefix(header, "Bearer ")
			if _, err := jwtService.Verify(token); err != nil {
				response.Error(w, http.StatusUnauthorized, "invalid_token", "invalid bearer token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
