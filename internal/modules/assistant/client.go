package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Usage is what one provider call consumed, as greenroot-ai reports it.
type Usage struct {
	Model             string  `json:"model"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	CachedInputTokens int     `json:"cached_input_tokens"`
	AudioSeconds      float64 `json:"audio_seconds"`
	Characters        int     `json:"characters"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatReply struct {
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls"`
	Usage     Usage      `json:"usage"`
}

// AIError is a failure the app can show: the AI service's code, or ours
// when the service could not be reached.
type AIError struct {
	Status  int
	Code    string
	Message string
}

func (e *AIError) Error() string { return e.Code + ": " + e.Message }

// aiService is greenroot-ai; tests swap in a fake.
type aiService interface {
	transcribe(ctx context.Context, requestID string, audio []byte, filename, contentType, language string) (string, Usage, error)
	chat(ctx context.Context, requestID string, messages []chatMessage, tools []toolSpec) (chatReply, error)
	speak(ctx context.Context, requestID, text, instructions string) ([]byte, Usage, error)
}

type httpAIService struct {
	base  string
	token string
	http  *http.Client
}

func newHTTPAIService(base, token string, timeout time.Duration) *httpAIService {
	return &httpAIService{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: timeout}}
}

func (s *httpAIService) do(ctx context.Context, requestID, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Internal-Token", s.token)
	req.Header.Set("X-Request-ID", requestID)
	// No retry here: a repeated provider call is a second charge.
	resp, err := s.http.Do(req)
	if err != nil {
		var netErr interface{ Timeout() bool }
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return nil, &AIError{http.StatusGatewayTimeout, "ai_timeout", "Mango took too long to answer"}
		}
		return nil, &AIError{http.StatusBadGateway, "ai_unavailable", "Mango is not reachable right now"}
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, serviceError(resp)
	}
	return resp, nil
}

func serviceError(resp *http.Response) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	code := body.Error.Code
	switch {
	case code == "ai_disabled" || code == "ai_not_configured" || code == "ai_quota_exceeded":
		return &AIError{http.StatusServiceUnavailable, code, "Mango is switched off for now"}
	case code == "ai_rate_limited":
		return &AIError{http.StatusTooManyRequests, code, "Mango is busy, try again in a minute"}
	case code == "ai_timeout":
		return &AIError{http.StatusGatewayTimeout, code, "Mango took too long to answer"}
	case code == "ai_bad_input" || code == "empty_audio" || code == "unsupported_audio":
		return &AIError{http.StatusUnprocessableEntity, "ai_bad_input", "Mango could not use this recording"}
	case resp.StatusCode == http.StatusUnauthorized:
		// Our token is wrong: a server setup problem, not the user's.
		return &AIError{http.StatusBadGateway, "ai_unavailable", "Mango is not reachable right now"}
	default:
		return &AIError{http.StatusBadGateway, "ai_provider_error", "Mango could not answer, try again"}
	}
}

func (s *httpAIService) transcribe(ctx context.Context, requestID string, audio []byte, filename, contentType, language string) (string, Usage, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="audio"; filename=%q`, filename)},
		"Content-Type":        {contentType},
	})
	if err != nil {
		return "", Usage{}, err
	}
	_, _ = part.Write(audio)
	if language != "" {
		_ = mw.WriteField("language", language)
	}
	_ = mw.Close()
	resp, err := s.do(ctx, requestID, "/v1/transcribe", mw.FormDataContentType(), &buf)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Text  string `json:"text"`
		Usage Usage  `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", Usage{}, &AIError{http.StatusBadGateway, "ai_provider_error", "Mango could not answer, try again"}
	}
	return out.Text, out.Usage, nil
}

func (s *httpAIService) chat(ctx context.Context, requestID string, messages []chatMessage, tools []toolSpec) (chatReply, error) {
	if tools == nil {
		tools = []toolSpec{} // the service wants a list; null is a 422
	}
	body, _ := json.Marshal(map[string]any{"messages": messages, "tools": tools})
	resp, err := s.do(ctx, requestID, "/v1/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return chatReply{}, err
	}
	defer resp.Body.Close()
	var out chatReply
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return chatReply{}, &AIError{http.StatusBadGateway, "ai_provider_error", "Mango could not answer, try again"}
	}
	return out, nil
}

func (s *httpAIService) speak(ctx context.Context, requestID, text, instructions string) ([]byte, Usage, error) {
	body, _ := json.Marshal(map[string]string{"text": text, "instructions": instructions})
	resp, err := s.do(ctx, requestID, "/v1/speech", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, Usage{}, err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || len(audio) == 0 {
		return nil, Usage{}, &AIError{http.StatusBadGateway, "ai_provider_error", "Mango could not read this aloud"}
	}
	var usage Usage
	_ = json.Unmarshal([]byte(resp.Header.Get("X-Usage")), &usage)
	return audio, usage, nil
}
