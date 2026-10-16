package auth

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/redis/go-redis/v9"
)

type fakeSMS struct {
	mu   sync.Mutex
	live bool
	err  error
	reqs []sms.SendRequest
}

func (f *fakeSMS) Live() bool { return f.live }
func (f *fakeSMS) Send(_ context.Context, req sms.SendRequest) (sms.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return sms.SendResult{Status: "SENT"}, f.err
}
func (f *fakeSMS) last(t *testing.T) sms.SendRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		t.Fatal("no SMS requested")
	}
	return f.reqs[len(f.reqs)-1]
}

func liveService(t *testing.T, repo *mockRepo) (*Service, *fakeSMS, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	sender := &fakeSMS{live: true}
	return NewService(repo, testJWT(), nil, client).WithSMS(sender), sender, server
}

func TestSendOTP_LiveUsesRandomCodeAndNoDevFallback(t *testing.T) {
	ctx := context.Background()
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s, sender, server := liveService(t, repo)

	resp, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "+91 90000 00000"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.MockOTP != "" {
		t.Fatal("live mode must never return mock_otp")
	}
	req := sender.last(t)
	code := req.Values["otp"]
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) || req.TemplateCode != sms.TemplateAuthLoginOTP {
		t.Fatalf("sms request = %+v", req)
	}
	if stored, _ := server.Get(redisutil.KeyOTP + "9000000000"); stored == code {
		t.Fatal("OTP must not be stored in clear text")
	}

	if code != mockOTP {
		if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9000000000", OTP: mockOTP}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("dev OTP must not work in live mode, got %v", err)
		}
	}
	if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9000000000", OTP: code}, ClientContext{}); err != nil {
		t.Fatalf("real OTP must verify: %v", err)
	}
	if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9000000000", OTP: code}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("OTP must be single-use, got %v", err)
	}
	// With no OTP stored, live mode must not fall back to the dev code.
	if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9000000000", OTP: mockOTP}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("missing OTP key must fail in live mode, got %v", err)
	}
}

func TestSendOTP_LiveNewNumberUsesRegisterTemplate(t *testing.T) {
	s, sender, _ := liveService(t, newMock())
	if _, err := s.SendOTP(context.Background(), SendOTPRequest{Mobile: "9876543210"}); err != nil {
		t.Fatal(err)
	}
	if got := sender.last(t); got.TemplateCode != sms.TemplateAuthRegisterOTP || got.UserID != nil {
		t.Fatalf("new number must get the registration OTP, got %+v", got)
	}
}

func TestSendOTP_LiveThrottlesPerNumber(t *testing.T) {
	ctx := context.Background()
	s, _, server := liveService(t, newMock())
	req := SendOTPRequest{Mobile: "9876543210"}

	if _, err := s.SendOTP(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendOTP(ctx, req); !errors.Is(err, ErrOTPCooldown) {
		t.Fatalf("immediate resend must hit cooldown, got %v", err)
	}
	for i := 2; i <= otpMaxPerHour; i++ {
		server.FastForward(otpResendCooldown + time.Second)
		if _, err := s.SendOTP(ctx, req); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	server.FastForward(otpResendCooldown + time.Second)
	if _, err := s.SendOTP(ctx, req); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("send %d must hit the hourly cap, got %v", otpMaxPerHour+1, err)
	}
	// A different number is unaffected.
	if _, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "9876543211"}); err != nil {
		t.Fatalf("other number throttled: %v", err)
	}
}

func TestVerifyOTP_BurnsAfterTooManyWrongGuesses(t *testing.T) {
	ctx := context.Background()
	s, sender, _ := liveService(t, newMock())
	if _, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "9876543210"}); err != nil {
		t.Fatal(err)
	}
	code := sender.last(t).Values["otp"]
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < otpMaxVerifyAttempts; i++ {
		if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9876543210", OTP: wrong}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9876543210", OTP: code}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("OTP must be burned after %d wrong guesses, got %v", otpMaxVerifyAttempts, err)
	}
}

func TestSendOTP_LiveFailsClosed(t *testing.T) {
	ctx := context.Background()

	s, sender, server := liveService(t, newMock())
	sender.err = errors.New("gateway down")
	if _, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "9876543210"}); !errors.Is(err, ErrOTPUnavailable) {
		t.Fatalf("SMS failure must surface, got %v", err)
	}
	if server.Exists(redisutil.KeyOTP + "9876543210") {
		t.Fatal("undelivered OTP must not stay valid")
	}

	noRedis := NewService(newMock(), testJWT(), nil).WithSMS(&fakeSMS{live: true})
	if _, err := noRedis.SendOTP(ctx, SendOTPRequest{Mobile: "9876543210"}); !errors.Is(err, ErrOTPUnavailable) {
		t.Fatalf("live OTP without Redis must fail, got %v", err)
	}
	if _, err := noRedis.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9876543210", OTP: mockOTP}, ClientContext{}); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("live OTP without Redis must not accept the dev code, got %v", err)
	}

	if _, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "12345"}); !errors.Is(err, ErrInvalidMobile) {
		t.Fatalf("non-Indian number must be rejected, got %v", err)
	}
}

func TestSendOTP_MockModeKeepsDevOTPAndLogsSMS(t *testing.T) {
	sender := &fakeSMS{}
	s := NewService(newMock(), testJWT(), nil).WithSMS(sender)
	resp, err := s.SendOTP(context.Background(), SendOTPRequest{Mobile: "9876543210"})
	if err != nil || resp.MockOTP != mockOTP {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if got := sender.last(t); got.Values["otp"] != mockOTP {
		t.Fatalf("mock mode SMS should carry the dev OTP, got %+v", got)
	}
	// Mock SMS failures never block login.
	sender.err = errors.New("no template")
	if _, err := s.SendOTP(context.Background(), SendOTPRequest{Mobile: "9876543210"}); err != nil {
		t.Fatalf("mock mode must ignore SMS errors: %v", err)
	}
}

type fakeEvents struct {
	mu     sync.Mutex
	events []messaging.Event
}

func (f *fakeEvents) NotifyAsync(ev messaging.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func TestVerifyOTP_NewUserRaisesWelcomeEvent(t *testing.T) {
	events := &fakeEvents{}
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s := NewService(repo, testJWT(), nil).WithSMS(&fakeSMS{}).WithEvents(events)

	if _, err := s.VerifyOTP(context.Background(), VerifyOTPRequest{Mobile: "9000000000", OTP: mockOTP}, ClientContext{}); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 0 {
		t.Fatalf("existing user must not get a welcome, got %+v", events.events)
	}
	if _, err := s.VerifyOTP(context.Background(), VerifyOTPRequest{Mobile: "9876543210", OTP: mockOTP}, ClientContext{}); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatalf("want one welcome event, got %d", len(events.events))
	}
	if ev := events.events[0]; ev.Code != sms.TemplateAccountWelcome || ev.Values["mobile"] != "9876543210" || ev.UserID == 0 {
		t.Fatalf("welcome event = %+v", ev)
	}
}
