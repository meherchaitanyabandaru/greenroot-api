// Package fcm sends push notifications through Firebase Cloud Messaging
// (HTTP v1 API), authenticated with a Google service account.
//
// Configure with FCM_SERVICE_ACCOUNT_JSON: the service-account key file's
// JSON (Firebase console → Project settings → Service accounts → Generate new
// private key). In production it lives in AWS Secrets Manager with the rest.
package fcm

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	scope         = "https://www.googleapis.com/auth/firebase.messaging"
	defaultTokens = "https://oauth2.googleapis.com/token" // #nosec G101 -- public OAuth endpoint URL, not a credential
	defaultSend   = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
)

// ErrUnregistered: the device token is no longer valid (app uninstalled,
// token rotated). The caller should stop sending to it.
var ErrUnregistered = errors.New("fcm: device token is no longer registered")

type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// Client sends messages. It is safe for concurrent use.
type Client struct {
	projectID string
	email     string
	key       *rsa.PrivateKey
	tokenURL  string
	sendURL   string
	http      *http.Client

	mu          sync.Mutex
	accessToken string
	expires     time.Time
}

// New parses a service-account JSON. An empty string returns nil, nil
// (push disabled; the caller decides whether that is allowed).
func New(serviceAccountJSON string) (*Client, error) {
	if strings.TrimSpace(serviceAccountJSON) == "" {
		return nil, nil
	}
	var sa serviceAccount
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return nil, fmt.Errorf("fcm: FCM_SERVICE_ACCOUNT_JSON is not a service-account JSON: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm: service account needs project_id, client_email and private_key")
	}
	key, err := parseKey(sa.PrivateKey)
	if err != nil {
		return nil, err
	}
	tokenURL := sa.TokenURI
	if tokenURL == "" {
		tokenURL = defaultTokens
	}
	return &Client{
		projectID: sa.ProjectID, email: sa.ClientEmail, key: key,
		tokenURL: tokenURL, sendURL: fmt.Sprintf(defaultSend, url.PathEscape(sa.ProjectID)),
		http: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func parseKey(p string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(p))
	if block == nil {
		return nil, errors.New("fcm: private_key is not PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("fcm: private_key is not an RSA key")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// Message is one notification to one device.
type Message struct {
	Token string
	Title string
	Body  string
	Data  map[string]string
}

// Send delivers m. ErrUnregistered means the token is dead.
func (c *Client) Send(ctx context.Context, m Message) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{"message": map[string]any{
		"token":        m.Token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         m.Data,
		// Ring like a message: the phone's default sound on both platforms
		// (without "sound" iOS delivers the banner silently).
		"android": map[string]any{"priority": "HIGH", "notification": map[string]any{
			"default_sound": true, "notification_priority": "PRIORITY_HIGH",
		}},
		"apns": map[string]any{
			"headers": map[string]string{"apns-priority": "10"},
			"payload": map[string]any{"aps": map[string]any{"sound": "default"}},
		},
	}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	// 404 UNREGISTERED, or 400 for a malformed/foreign token: never retry it.
	if resp.StatusCode == http.StatusNotFound || bytes.Contains(raw, []byte("UNREGISTERED")) ||
		(resp.StatusCode == http.StatusBadRequest && bytes.Contains(raw, []byte("registration token"))) {
		return ErrUnregistered
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.accessToken = "" // force a new OAuth token next time
		c.mu.Unlock()
	}
	return fmt.Errorf("fcm: send: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
}

// token returns a cached OAuth access token, refreshing it 5 minutes early.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Until(c.expires) > 5*time.Minute {
		return c.accessToken, nil
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": c.email, "scope": scope, "aud": c.tokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("fcm: sign assertion: %w", err)
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: oauth: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out); err != nil || resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("fcm: oauth: HTTP %d %s", resp.StatusCode, out.Error)
	}
	c.accessToken, c.expires = out.AccessToken, now.Add(time.Duration(out.ExpiresIn)*time.Second)
	return c.accessToken, nil
}
