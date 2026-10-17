package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/clientip"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// serve runs h behind clientip.Middleware with no trusted proxies, as in a
// deployment where the API is reached directly.
func serve(h http.Handler, r *http.Request) int {
	rec := httptest.NewRecorder()
	clientip.Middleware(nil)(h).ServeHTTP(rec, r)
	return rec.Code
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestRateLimitCannotBeBypassedWithNewConnectionsOrHeaders(t *testing.T) {
	h := RateLimit(newTestRedis(t), "test", 3, time.Minute)(okHandler())

	codes := make([]int, 0, 5)
	for i := range 5 {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		// A new source port per request (new TCP connection) and a forged
		// X-Forwarded-For must still count against the same client.
		r.RemoteAddr = "203.0.113.7:" + strconv.Itoa(i+1) + "000"
		r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
		codes = append(codes, serve(h, r))
	}
	want := []int{200, 200, 200, 429, 429}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %v, want %v", codes, want)
		}
	}
}

func TestOTPRateLimitKeysOnMobileInBody(t *testing.T) {
	var bodies []string
	h := OTPRateLimit(newTestRedis(t))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusOK)
	}))

	send := func(remote, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/send-otp", strings.NewReader(body))
		r.RemoteAddr = remote
		return serve(h, r)
	}

	// Five sends to one mobile are allowed, from different IPs; the sixth is not.
	for i := range 5 {
		if code := send("203.0.113."+strconv.Itoa(i+1)+":1", `{"mobile":"9100000000"}`); code != 200 {
			t.Fatalf("send %d = %d, want 200", i+1, code)
		}
	}
	if code := send("203.0.113.9:1", `{"mobile":"9100000000"}`); code != http.StatusTooManyRequests {
		t.Fatalf("6th send = %d, want 429", code)
	}
	// Another mobile is unaffected.
	if code := send("203.0.113.9:1", `{"mobile":"9300000000"}`); code != 200 {
		t.Fatalf("other mobile = %d, want 200", code)
	}
	// The handler still receives the full body.
	if bodies[0] != `{"mobile":"9100000000"}` {
		t.Fatalf("handler body = %q", bodies[0])
	}
}

func TestMobileFromBodyFallsBackToIP(t *testing.T) {
	for _, body := range []string{"", "not json", `{"other":1}`, `{"mobile":"  "}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.RemoteAddr = "203.0.113.7:5"
		if got := mobileFromBody(r); got != "203.0.113.7" {
			t.Fatalf("body %q: key = %q, want client IP", body, got)
		}
		rest, _ := io.ReadAll(r.Body)
		if string(rest) != body {
			t.Fatalf("body %q not restored: %q", body, rest)
		}
	}
}

func TestMobileFromBodyRestoresLargeBody(t *testing.T) {
	large := `{"mobile":"9100000000","pad":"` + strings.Repeat("x", otpPeekBytes*2) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(large))
	r.RemoteAddr = "203.0.113.7:5"
	_ = mobileFromBody(r) // truncated JSON: falls back to IP, but must not lose bytes
	rest, _ := io.ReadAll(r.Body)
	if string(rest) != large {
		t.Fatalf("restored %d bytes, want %d", len(rest), len(large))
	}
}
