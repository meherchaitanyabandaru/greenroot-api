package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func testClient(t *testing.T, send http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		_ = r.ParseForm()
		claims := jwt.MapClaims{}
		if _, err := jwt.ParseWithClaims(r.Form.Get("assertion"), claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }); err != nil || claims["scope"] != scope {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"ya29.test","expires_in":3600}`)
	})
	mux.HandleFunc("/send", send)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	sa, _ := json.Marshal(map[string]string{
		"project_id": "greenroot-test", "client_email": "push@greenroot-test.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   srv.URL + "/token",
	})
	c, err := New(string(sa))
	if err != nil {
		t.Fatal(err)
	}
	c.sendURL = srv.URL + "/send"
	return c, &tokenCalls
}

func TestSendAuthenticatesAndCachesToken(t *testing.T) {
	var got map[string]map[string]any
	c, tokenCalls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ya29.test" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"name":"projects/greenroot-test/messages/1"}`)
	})
	for i := 0; i < 3; i++ {
		if err := c.Send(context.Background(), Message{Token: "device-1", Title: "Order delivered", Body: "DSP-1", Data: map[string]string{"type": "ORDER_DELIVERED"}}); err != nil {
			t.Fatal(err)
		}
	}
	if n := tokenCalls.Load(); n != 1 {
		t.Fatalf("OAuth token requested %d times, want once (cached)", n)
	}
	if got["message"]["token"] != "device-1" {
		t.Fatalf("payload: %v", got)
	}
	// Both platforms ring: Android's default sound, iOS's "default".
	raw, _ := json.Marshal(got["message"])
	for _, want := range []string{`"default_sound":true`, `"sound":"default"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("payload has no %s: %s", want, raw)
		}
	}
}

func TestSendReportsDeadTokens(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`)
	})
	if err := c.Send(context.Background(), Message{Token: "gone"}); !errors.Is(err, ErrUnregistered) {
		t.Fatalf("got %v, want ErrUnregistered", err)
	}
}

func TestSendOtherErrorsAreRetryable(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"status":"UNAVAILABLE"}}`, http.StatusServiceUnavailable)
	})
	err := c.Send(context.Background(), Message{Token: "device-1"})
	if err == nil || errors.Is(err, ErrUnregistered) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("got %v, want a retryable 503 error", err)
	}
}

func TestNewValidatesServiceAccount(t *testing.T) {
	if c, err := New(""); c != nil || err != nil {
		t.Fatal("empty config must mean push disabled, no error")
	}
	for _, bad := range []string{"not json", `{"project_id":"p"}`, `{"project_id":"p","client_email":"e","private_key":"nope"}`} {
		if _, err := New(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
