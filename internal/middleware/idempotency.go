package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/redis/go-redis/v9"
)

const (
	idempotencyTTL     = 24 * time.Hour
	idempotencyPending = "pending"
)

type storedResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}

// Idempotency replays the result of a POST the client already sent, so a
// retry after a lost response (weak network) cannot create a second order.
//
// Opt-in: only signed-in POSTs with an Idempotency-Key header (8-64 chars) are
// tracked, per user, method and path, for 24 hours. Only successes are
// remembered: after a 4xx/5xx the key is released so a corrected request runs.
// The same key while the first request is still running gets 409. Without
// Redis, or if Redis fails, requests pass through unchanged.
func Idempotency(rdb redis.Cmdable) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			actor, signedIn := authctx.ActorFromContext(r.Context())
			if rdb == nil || r.Method != http.MethodPost || key == "" || !signedIn {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) < 8 || len(key) > 64 {
				response.Error(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be 8-64 characters")
				return
			}
			sum := sha256.Sum256([]byte(strings.Join([]string{strconv.FormatInt(actor.UserID, 10), r.Method, r.URL.Path, key}, "|")))
			rk := "idem:" + hex.EncodeToString(sum[:])
			ctx := r.Context()

			fresh, err := rdb.SetNX(ctx, rk, idempotencyPending, idempotencyTTL).Result()
			if err != nil {
				next.ServeHTTP(w, r) // fail open: Redis trouble must not block real work
				return
			}
			if !fresh {
				replay(w, rdb, ctx, rk)
				return
			}

			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			stored := false
			defer func() {
				if !stored { // error status or a panic: let a retry run
					rdb.Del(context.WithoutCancel(ctx), rk)
				}
			}()
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 300 {
				data, _ := json.Marshal(storedResponse{Status: rec.status, ContentType: rec.Header().Get("Content-Type"), Body: rec.body.Bytes()})
				stored = rdb.Set(context.WithoutCancel(ctx), rk, data, idempotencyTTL).Err() == nil
			}
		})
	}
}

func replay(w http.ResponseWriter, rdb redis.Cmdable, ctx context.Context, rk string) {
	raw, err := rdb.Get(ctx, rk).Result()
	if err != nil || raw == idempotencyPending {
		response.Error(w, http.StatusConflict, "request_in_progress", "This request is still being processed. Check before trying again.")
		return
	}
	var s storedResponse
	if json.Unmarshal([]byte(raw), &s) != nil {
		response.Error(w, http.StatusConflict, "request_in_progress", "This request is still being processed. Check before trying again.")
		return
	}
	if s.ContentType != "" {
		w.Header().Set("Content-Type", s.ContentType)
	}
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(s.Status)
	_, _ = w.Write(s.Body)
}

// recorder passes the response through and keeps a copy to replay.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
