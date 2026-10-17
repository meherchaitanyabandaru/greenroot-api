package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/clientip"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/redis/go-redis/v9"
)

// RateLimit returns a middleware that limits requests to maxReqs per window per
// key derived by keyFunc (e.g. client IP or mobile number from body).
//
// When rdb is nil the middleware is a no-op — the API works without Redis.
func RateLimit(rdb *redis.Client, prefix string, maxReqs int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rdb == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := fmt.Sprintf("rl:%s:%s", prefix, clientKey(r))
			count, err := rdb.Incr(r.Context(), key).Result()
			if err != nil {
				// Redis error → let the request through rather than hard-failing.
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				rdb.Expire(r.Context(), key, window)
			}
			if count > int64(maxReqs) {
				response.Error(w, http.StatusTooManyRequests, "rate_limited", "too many requests — please try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OTPRateLimit limits OTP sends to 5 per mobile per 10 minutes. It reads the
// mobile number from the JSON body field "mobile" (falling back to the client
// IP) and leaves the body intact for the handler.
//
// When rdb is nil the middleware is a no-op.
func OTPRateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rdb == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mobile := mobileFromBody(r)
			key := fmt.Sprintf("rl:otp:%s", mobile)
			count, err := incrWithExpiry(r.Context(), rdb, key, 10*time.Minute)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if count > 5 {
				response.Error(w, http.StatusTooManyRequests, "otp_rate_limited", "too many OTP requests — please wait 10 minutes")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// VerifyRateLimit limits OTP verify attempts to 10 per IP per 15 minutes.
func VerifyRateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return RateLimit(rdb, "verify", 10, 15*time.Minute)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func incrWithExpiry(ctx context.Context, rdb *redis.Client, key string, window time.Duration) (int64, error) {
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 {
		rdb.Expire(ctx, key, window)
	}
	return count, nil
}

// clientKey is the client IP without the port, so one client shares one
// bucket across connections, and proxy headers count only from trusted proxies.
func clientKey(r *http.Request) string {
	return clientip.From(r)
}

// otpPeekBytes bounds how much of the body is read to find "mobile"; a
// send-otp body is a few dozen bytes.
const otpPeekBytes = 4 << 10

// mobileFromBody returns the "mobile" field of a JSON body, or the client IP
// when the body has none. The body is restored so the handler reads it whole.
func mobileFromBody(r *http.Request) string {
	if r.Body == nil {
		return clientKey(r)
	}
	head, err := io.ReadAll(io.LimitReader(r.Body, otpPeekBytes))
	r.Body = readCloser{io.MultiReader(bytes.NewReader(head), r.Body), r.Body}
	if err != nil {
		return clientKey(r)
	}
	var req struct {
		Mobile string `json:"mobile"`
	}
	if json.Unmarshal(head, &req) != nil {
		return clientKey(r)
	}
	if m := strings.TrimSpace(req.Mobile); m != "" {
		return m
	}
	return clientKey(r)
}

// readCloser reads from a replayed body but closes the original one.
type readCloser struct {
	io.Reader
	io.Closer
}
