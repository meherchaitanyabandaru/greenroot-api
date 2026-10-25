package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	var dest struct {
		Name string `json:"name"`
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Ravi"}`))
	if !DecodeJSON(w, r, &dest) || dest.Name != "Ravi" || w.Body.Len() != 0 {
		t.Fatalf("valid body: ok=%v name=%q body=%q", dest.Name == "Ravi", dest.Name, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
	if DecodeJSON(w, r, &dest) {
		t.Fatal("malformed body accepted")
	}
	want := `{"error":{"code":"invalid_json","message":"invalid JSON request body"}}` + "\n"
	if w.Code != http.StatusBadRequest || w.Body.String() != want || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("got %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
}

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, Envelope{"status": "ok"})
	if w.Code != http.StatusOK || w.Body.String() != `{"status":"ok"}`+"\n" {
		t.Errorf("got %d %q", w.Code, w.Body.String())
	}
}
