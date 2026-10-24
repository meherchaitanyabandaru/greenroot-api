package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/redis/go-redis/v9"
)

// counting handler: answers with the given status and how many times it ran.
func countingHandler(status int, runs *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*runs++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"run":` + string(rune('0'+*runs)) + `}`))
	})
}

func idemRequest(user int64, method, key string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/orders", strings.NewReader(`{}`))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if user > 0 {
		r = r.WithContext(authctx.StoreActor(r.Context(), authctx.Actor{UserID: user}))
	}
	return r
}

func newIdem(t *testing.T) (redis.Cmdable, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func TestIdempotency_RetryReplaysTheFirstResult(t *testing.T) {
	rdb, _ := newIdem(t)
	runs := 0
	h := Idempotency(rdb)(countingHandler(http.StatusCreated, &runs))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, idemRequest(7, http.MethodPost, "key-12345678"))
	again := httptest.NewRecorder()
	h.ServeHTTP(again, idemRequest(7, http.MethodPost, "key-12345678"))

	if runs != 1 {
		t.Fatalf("handler ran %d times, want 1 (a retry must not create a second order)", runs)
	}
	if again.Code != http.StatusCreated || again.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %q, want %d %q", again.Code, again.Body, first.Code, first.Body)
	}
	if again.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("replay not marked")
	}
}

func TestIdempotency_ErrorsAreNotRemembered(t *testing.T) {
	rdb, _ := newIdem(t)
	runs := 0
	h := Idempotency(rdb)(countingHandler(http.StatusBadRequest, &runs))
	for i := 0; i < 2; i++ {
		h.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, "key-12345678"))
	}
	if runs != 2 {
		t.Fatalf("handler ran %d times, want 2 (a corrected request must run)", runs)
	}
}

func TestIdempotency_SameKeyWhileRunningIs409(t *testing.T) {
	rdb, mr := newIdem(t)
	runs := 0
	h := Idempotency(rdb)(countingHandler(http.StatusCreated, &runs))
	// Simulate the first request still in flight: claim the key as pending.
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, "key-12345678"))
	for _, k := range mr.Keys() {
		_ = mr.Set(k, idempotencyPending)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, idemRequest(7, http.MethodPost, "key-12345678"))
	if rec.Code != http.StatusConflict || runs != 1 {
		t.Fatalf("got %d after %d runs, want 409 after 1", rec.Code, runs)
	}
}

func TestIdempotency_PanicReleasesTheKey(t *testing.T) {
	rdb, mr := newIdem(t)
	boom := Idempotency(rdb)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	func() {
		defer func() { _ = recover() }()
		boom.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, "key-12345678"))
	}()
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("key left behind after a panic: %v (retries would get 409 for 24 h)", keys)
	}
}

func TestIdempotency_ScopeAndOptIn(t *testing.T) {
	rdb, _ := newIdem(t)
	runs := 0
	h := Idempotency(rdb)(countingHandler(http.StatusCreated, &runs))
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, "key-12345678"))
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(8, http.MethodPost, "key-12345678")) // another user
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, ""))             // no key
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(7, http.MethodPost, ""))
	h.ServeHTTP(httptest.NewRecorder(), idemRequest(0, http.MethodPost, "key-12345678")) // signed out
	if runs != 5 {
		t.Fatalf("handler ran %d times, want 5 (keys are per user; no key or signed out = untracked)", runs)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, idemRequest(7, http.MethodPost, "short"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short key: got %d, want 400", rec.Code)
	}
}

func TestIdempotency_RedisDownPassesThrough(t *testing.T) {
	rdb, mr := newIdem(t)
	mr.Close()
	runs := 0
	h := Idempotency(rdb)(countingHandler(http.StatusCreated, &runs))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, idemRequest(7, http.MethodPost, "key-12345678"))
	if rec.Code != http.StatusCreated || runs != 1 {
		t.Fatalf("got %d after %d runs, want the request to run normally", rec.Code, runs)
	}
}
