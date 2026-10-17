package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func echoLen() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest) // what decodeJSON does on a read error
			return
		}
		_ = json.NewEncoder(w).Encode(n)
	})
}

func send(h http.Handler, body string, contentType string, chunked bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if chunked {
		req.ContentLength = -1
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBodyLimit(t *testing.T) {
	h := BodyLimit(10, 100)(echoLen())

	if rec := send(h, strings.Repeat("a", 10), "application/json", false); rec.Code != http.StatusOK {
		t.Fatalf("body at the limit rejected: %d", rec.Code)
	}
	if rec := send(h, strings.Repeat("a", 11), "application/json", false); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversize JSON = %d, want 413", rec.Code)
	}
	if rec := send(h, strings.Repeat("a", 11), "application/json", true); rec.Code != http.StatusBadRequest {
		t.Fatalf("chunked oversize JSON must be cut off, got %d", rec.Code)
	}
	if rec := send(h, strings.Repeat("a", 50), "multipart/form-data; boundary=x", false); rec.Code != http.StatusOK {
		t.Fatalf("upload under the upload limit rejected: %d", rec.Code)
	}
	if rec := send(h, strings.Repeat("a", 101), "multipart/form-data; boundary=x", false); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload = %d, want 413", rec.Code)
	}
}
