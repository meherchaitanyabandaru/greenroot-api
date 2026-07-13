package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

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

// OTPRateLimit is a higher-level wrapper that limits OTP sends to 5 per mobile
// per 10 minutes. It reads the mobile number from the JSON body field "mobile"
// and falls back to IP if not present.
//
// When rdb is nil the middleware is a no-op.
func OTPRateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rdb == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mobile := mobileFromQuery(r)
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

func clientKey(r *http.Request) string {
	// RealIP middleware has already resolved X-Forwarded-For → RemoteAddr
	if ip := r.RemoteAddr; ip != "" {
		return ip
	}
	return "unknown"
}

func mobileFromQuery(r *http.Request) string {
	if m := r.URL.Query().Get("mobile"); m != "" {
		return m
	}
	return clientKey(r)
}
