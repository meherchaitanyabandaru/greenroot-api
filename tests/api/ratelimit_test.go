package apitest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestRateLimits_CannotBeBypassed checks the real limits end to end: forged
// proxy headers and fresh connections must not reset a client's bucket.
func TestRateLimits_CannotBeBypassed(t *testing.T) {
	if testRedis == nil {
		t.Skip("API runs without Redis: rate limits are disabled")
	}
	resetRateLimits(t)
	t.Cleanup(func() { resetRateLimits(t) })

	verify := func(i int) int {
		b, _ := json.Marshal(map[string]string{"mobile": "9999999999", "otp": "000000"})
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/verify-otp", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i))
		req.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(i))
		req.Close = true // a new TCP connection (new source port) every time
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for i := 1; i <= 10; i++ {
		if code := verify(i); code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d limited early", i)
		}
	}
	if code := verify(11); code != http.StatusTooManyRequests {
		t.Fatalf("11th verify attempt = %d, want 429", code)
	}

	// send-otp is limited per mobile (5 per 10 minutes), regardless of IP.
	resetRateLimits(t)
	send := func() int {
		resp := post(t, "/api/v1/auth/send-otp", map[string]string{"mobile": "9300000000"}, "")
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for i := 1; i <= 5; i++ {
		if code := send(); code == http.StatusTooManyRequests {
			t.Fatalf("send %d limited early", i)
		}
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("6th send-otp = %d, want 429", code)
	}
}
