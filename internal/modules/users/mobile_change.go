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
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
)

// Changing the sign-in number: a code goes by SMS to the NEW number, bound
// to this user and that number. Same limits as the login OTP.
const (
	mobileCodeTTL         = 5 * time.Minute
	mobileCodeCooldown    = 30 * time.Second
	mobileCodeMaxPerHour  = 5
	mobileCodeMaxAttempts = 5
	mobileDevCode         = "123456"
	mobileOTPPrefix       = "mobile_change_otp:"
)

var (
	ErrInvalidMobile     = errors.New("enter a 10-digit Indian mobile number")
	ErrSameMobile        = errors.New("this is already your mobile number")
	ErrMobileTaken       = errors.New("mobile number already used by another account")
	ErrInvalidMobileCode = errors.New("invalid mobile verification code")
	ErrSMSUnavailable    = errors.New("sms delivery unavailable")
)

// SMSSender is the slice of the sms module users needs.
type SMSSender interface {
	Live() bool
	Send(ctx context.Context, req sms.SendRequest) (sms.SendResult, error)
}

type MobileCodeRequest struct {
	Mobile string `json:"mobile"`
}

type VerifyMobileRequest struct {
	Mobile string `json:"mobile"`
	Code   string `json:"code"`
}

type MobileCodeResponse struct {
	Message     string `json:"message"`
	ExpiresIn   int    `json:"expires_in_seconds"`
	MockCode    string `json:"mock_code,omitempty"`
	DeliveredTo string `json:"delivered_to"`
}

// WithSMS enables changing the sign-in mobile number.
func (s *Service) WithSMS(sender SMSSender) *Service {
	s.sms = sender
	s.mobileOTPs = otp.New(s.redis, otp.Policy{
		Prefix:      mobileOTPPrefix,
		TTL:         mobileCodeTTL,
		Cooldown:    mobileCodeCooldown,
		MaxPerHour:  mobileCodeMaxPerHour,
		MaxAttempts: mobileCodeMaxAttempts,
	})
	return s
}

func (s *Service) liveSMS() bool { return s.sms != nil && s.sms.Live() }

func mobileCodeID(userID int64, mobile string) string {
	return strconv.FormatInt(userID, 10) + ":" + mobile
}

// checkNewMobile normalizes the number and rejects the current one and one
// another account uses.
func (s *Service) checkNewMobile(ctx context.Context, userID int64, raw string) (string, error) {
	mobile, ok := phone.Normalize(raw)
	if !ok {
		return "", ErrInvalidMobile
	}
	current, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if current.Mobile == mobile {
		return "", ErrSameMobile
	}
	taken, err := s.repository.MobileTakenByOther(ctx, mobile, userID)
	if err != nil {
		return "", err
	}
	if taken {
		return "", ErrMobileTaken
	}
	return mobile, nil
}

// SendMobileCode texts a 6-digit code to the new number.
func (s *Service) SendMobileCode(ctx context.Context, actor ActorContext, req MobileCodeRequest) (MobileCodeResponse, error) {
	if s.sms == nil {
		return MobileCodeResponse{}, ErrSMSUnavailable
	}
	mobile, err := s.checkNewMobile(ctx, actor.UserID, req.Mobile)
	if err != nil {
		return MobileCodeResponse{}, err
	}
	id := mobileCodeID(actor.UserID, mobile)
	code := mobileDevCode
	if s.liveSMS() {
		if !s.mobileOTPs.Enabled() {
			slog.Error("live mobile change requires Redis; refusing to send")
			return MobileCodeResponse{}, ErrSMSUnavailable
		}
		if err := s.mobileOTPs.Throttle(ctx, id); err != nil {
			return MobileCodeResponse{}, err
		}
		if code, err = otp.Generate(); err != nil {
			return MobileCodeResponse{}, err
		}
	}
	if err := s.mobileOTPs.Save(ctx, id, code); err != nil {
		if s.liveSMS() {
			return MobileCodeResponse{}, err
		}
		slog.Warn("redis mobile code set failed; falling back to dev code", "error", err)
	}
	userID := actor.UserID
	_, err = s.sms.Send(ctx, sms.SendRequest{
		TemplateCode:  sms.TemplateAuthRegisterOTP,
		Mobile:        mobile,
		UserID:        &userID,
		Values:        map[string]string{"otp": code, "valid_minutes": strconv.Itoa(int(mobileCodeTTL / time.Minute))},
		Sensitive:     []string{"otp"},
		ReferenceType: "MOBILE_CHANGE",
		ReferenceID:   &userID,
	})
	resp := MobileCodeResponse{Message: "Verification code sent", ExpiresIn: int(mobileCodeTTL / time.Second), DeliveredTo: mobile}
	if !s.liveSMS() {
		resp.MockCode = mobileDevCode
		return resp, nil
	}
	if err != nil {
		s.mobileOTPs.Delete(ctx, id)
		slog.Error("mobile change code send failed", "error", err)
		return MobileCodeResponse{}, ErrSMSUnavailable
	}
	return resp, nil
}

// VerifyMobile checks the code and makes the new number the sign-in number.
func (s *Service) VerifyMobile(ctx context.Context, actor ActorContext, req VerifyMobileRequest) (User, error) {
	if s.sms == nil {
		return User{}, ErrSMSUnavailable
	}
	mobile, err := s.checkNewMobile(ctx, actor.UserID, req.Mobile)
	if err != nil {
		return User{}, err
	}
	devCode := mobileDevCode
	if s.liveSMS() {
		devCode = ""
	}
	if !s.mobileOTPs.Verify(ctx, mobileCodeID(actor.UserID, mobile), strings.TrimSpace(req.Code), devCode) {
		return User{}, ErrInvalidMobileCode
	}
	user, err := s.repository.ChangeMobile(ctx, actor.UserID, mobile, time.Now())
	if err != nil {
		return User{}, fmt.Errorf("change mobile: %w", err)
	}
	s.recordChange(ctx, actor, "users", user.ID, "UPDATE", activityChangeMobile, "USER", user.ID, map[string]any{"mobile_changed": true})
	return *user, nil
}
