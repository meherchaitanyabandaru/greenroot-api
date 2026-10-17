package middleware

import (
	"mime"
	"net/http"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// BodyLimit caps request bodies: maxBody for JSON and other bodies,
// maxUpload for multipart uploads (avatar, quotation PDF). A declared
// Content-Length over the cap is rejected with 413 before reading; chunked
// bodies are cut off by MaxBytesReader, which makes the handler's decode fail.
func BodyLimit(maxBody, maxUpload int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil || r.Body == http.NoBody {
				next.ServeHTTP(w, r)
				return
			}
			limit := maxBody
			if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
				limit = maxUpload
			}
			if r.ContentLength > limit {
				response.Error(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body is too large")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
