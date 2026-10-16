package users

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/otp"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/email"
)

const (
	emailCodeTTL         = 10 * time.Minute
	emailCodeCooldown    = 30 * time.Second
	emailCodeMaxPerHour  = 5
	emailCodeMaxAttempts = 5
	emailDevCode         = "123456"
	emailOTPPrefix       = "email_otp:"
)

var (
	ErrEmailTaken       = errors.New("email already used by another account")
	ErrInvalidEmailCode = errors.New("invalid email verification code")
	ErrEmailUnavailable = errors.New("email delivery unavailable")
	ErrEmailCooldown    = otp.ErrCooldown
	ErrEmailRateLimited = otp.ErrRateLimited
)

// EmailSender is the slice of the email module users needs.
type EmailSender interface {
	Live() bool
	Send(ctx context.Context, req email.SendRequest) (email.SendResult, error)
}

type EmailCodeRequest struct {
	Email string `json:"email"`
}

type VerifyEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type EmailCodeResponse struct {
	Message     string `json:"message"`
	ExpiresIn   int    `json:"expires_in_seconds"`
	MockCode    string `json:"mock_code,omitempty"`
	DeliveredTo string `json:"delivered_to"`
}

// WithEmail enables email verification codes.
func (s *Service) WithEmail(sender EmailSender) *Service {
	s.email = sender
	s.emailOTPs = otp.New(s.redis, otp.Policy{
		Prefix:      emailOTPPrefix,
		TTL:         emailCodeTTL,
		Cooldown:    emailCodeCooldown,
		MaxPerHour:  emailCodeMaxPerHour,
		MaxAttempts: emailCodeMaxAttempts,
	})
	return s
}

func (s *Service) liveEmail() bool { return s.email != nil && s.email.Live() }

// The code is bound to both the user and the address, so it cannot verify a
// different address or another account.
func emailCodeID(userID int64, addr string) string {
	return strconv.FormatInt(userID, 10) + ":" + addr
}

// SendEmailCode emails a 6-digit code proving the user owns the address.
func (s *Service) SendEmailCode(ctx context.Context, actor ActorContext, req EmailCodeRequest) (EmailCodeResponse, error) {
	if s.email == nil {
		return EmailCodeResponse{}, ErrEmailUnavailable
	}
	addr, ok := email.NormalizeAddress(req.Email)
	if !ok {
		return EmailCodeResponse{}, ErrInvalidInput
	}
	taken, err := s.repository.EmailTakenByOther(ctx, addr, actor.UserID)
	if err != nil {
		return EmailCodeResponse{}, err
	}
	if taken {
		return EmailCodeResponse{}, ErrEmailTaken
	}

	id := emailCodeID(actor.UserID, addr)
	code := emailDevCode
	if s.liveEmail() {
		if !s.emailOTPs.Enabled() {
			slog.Error("live email verification requires Redis; refusing to send")
			return EmailCodeResponse{}, ErrEmailUnavailable
		}
		if err := s.emailOTPs.Throttle(ctx, id); err != nil {
			return EmailCodeResponse{}, err
		}
		if code, err = otp.Generate(); err != nil {
			return EmailCodeResponse{}, err
		}
	}
	if err := s.emailOTPs.Save(ctx, id, code); err != nil {
		if s.liveEmail() {
			return EmailCodeResponse{}, err
		}
		slog.Warn("redis email code set failed; falling back to dev code", "error", err)
	}

	userID := actor.UserID
	_, err = s.email.Send(ctx, email.SendRequest{
		TemplateCode:  email.TemplateEmailVerifyOTP,
		To:            addr,
		UserID:        &userID,
		Values:        map[string]string{"otp": code, "valid_minutes": strconv.Itoa(int(emailCodeTTL / time.Minute))},
		Sensitive:     []string{"otp"},
		ReferenceType: "EMAIL_VERIFY",
		ReferenceID:   &userID,
	})
	resp := EmailCodeResponse{Message: "Verification code sent", ExpiresIn: int(emailCodeTTL / time.Second), DeliveredTo: addr}
	if !s.liveEmail() {
		// Mock mode: the fixed dev code works even if the mock send was blocked.
		resp.MockCode = emailDevCode
		return resp, nil
	}
	if err != nil {
		s.emailOTPs.Delete(ctx, id)
		slog.Error("email verification send failed", "error", err)
		return EmailCodeResponse{}, ErrEmailUnavailable
	}
	return resp, nil
}

// VerifyEmail checks the code and saves the address as verified.
func (s *Service) VerifyEmail(ctx context.Context, actor ActorContext, req VerifyEmailRequest) (User, error) {
	if s.email == nil {
		return User{}, ErrEmailUnavailable
	}
	addr, ok := email.NormalizeAddress(req.Email)
	if !ok {
		return User{}, ErrInvalidInput
	}
	devCode := emailDevCode
	if s.liveEmail() {
		devCode = ""
	}
	if !s.emailOTPs.Verify(ctx, emailCodeID(actor.UserID, addr), strings.TrimSpace(req.Code), devCode) {
		return User{}, ErrInvalidEmailCode
	}
	user, err := s.repository.SetVerifiedEmail(ctx, actor.UserID, addr, time.Now())
	if err != nil {
		return User{}, fmt.Errorf("save verified email: %w", err)
	}
	s.recordChange(ctx, actor, "users", user.ID, "UPDATE", activityVerifyEmail, "USER", user.ID, map[string]any{"email_verified": true})
	return *user, nil
}
