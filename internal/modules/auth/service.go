package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/otp"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

// issueTokenPair fetches fresh token context from DB and signs both tokens.
// This is the ONE place we touch the DB for auth state — never again per request.
func (s *Service) issueTokenPair(ctx context.Context, user *User, sessionID int64) (accessToken, refreshToken string, err error) {
	tc, err := s.repository.GetTokenContext(ctx, user.ID)
	if err != nil {
		return "", "", err
	}
	jtc := jwtplatform.TokenContext{
		UserStatus:      tc.UserStatus,
		NurseryID:       tc.NurseryID,
		NurseryStatus:   tc.NurseryStatus,
		SubTier:         tc.SubTier,
		SubExpiresEpoch: tc.SubExpiresEpoch,
	}
	userIDStr := strconv.FormatInt(user.ID, 10)
	sessionIDStr := strconv.FormatInt(sessionID, 10)
	accessToken, err = s.jwt.IssueAccessToken(userIDStr, sessionIDStr, user.Mobile, user.Roles, jtc)
	if err != nil {
		return "", "", err
	}
	refreshToken, err = s.jwt.IssueRefreshToken(userIDStr, sessionIDStr, user.Mobile, user.Roles, jtc)
	return
}

var (
	ErrInvalidOTP          = errors.New("invalid otp")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrInvalidToken        = errors.New("invalid token")
	ErrForbidden           = apperrs.ErrForbidden
	ErrInvalidMobile       = errors.New("invalid mobile")
	ErrOTPCooldown         = otp.ErrCooldown
	ErrOTPRateLimited      = otp.ErrRateLimited
	ErrOTPUnavailable      = errors.New("otp delivery unavailable")
)

// SMSSender is the slice of the sms module that auth needs.
type SMSSender interface {
	Live() bool
	Send(ctx context.Context, req sms.SendRequest) (sms.SendResult, error)
}

// EventNotifier raises cross-channel events (welcome SMS/email).
type EventNotifier interface {
	NotifyAsync(ev messaging.Event)
}

type Service struct {
	repository Repository
	jwt        *jwtplatform.Service
	audit      *auditlog.Service
	redis      redis.Cmdable
	otps       *otp.Store
	sms        SMSSender
	events     EventNotifier
}

type ClientContext struct {
	IPAddress string
	UserAgent string
}

func NewService(repository Repository, jwt *jwtplatform.Service, audit *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, jwt: jwt, audit: audit, redis: rdb, otps: otp.New(rdb, otp.Policy{
		Prefix:      redisutil.KeyOTP,
		TTL:         otpTTL,
		Cooldown:    otpResendCooldown,
		MaxPerHour:  otpMaxPerHour,
		MaxAttempts: otpMaxVerifyAttempts,
	})}
}

// WithEvents attaches the cross-channel notifier used for the welcome message.
func (s *Service) WithEvents(n EventNotifier) *Service {
	s.events = n
	return s
}

// WithSMS attaches OTP/welcome SMS delivery. Without it (or with a mock
// provider) the fixed dev OTP is used and returned as mock_otp.
func (s *Service) WithSMS(sender SMSSender) *Service {
	s.sms = sender
	return s
}

// liveOTP is true only when OTPs really reach handsets: random codes, no dev
// fallback, no mock_otp in responses.
func (s *Service) liveOTP() bool { return s.sms != nil && s.sms.Live() }

func (s *Service) SendOTP(ctx context.Context, req SendOTPRequest) (SendOTPResponse, error) {
	mobile := otpMobile(req.Mobile)
	if !s.liveOTP() {
		if err := s.otps.Save(ctx, mobile, mockOTP); err != nil {
			slog.Warn("redis otp set failed; falling back to dev otp verification", "error", err)
		}
		// Logged as MOCKED; failures are irrelevant because the dev OTP is fixed.
		_ = s.sendOTPSMS(ctx, mobile, mockOTP)
		return SendOTPResponse{
			Message: "OTP sent successfully",
			MockOTP: mockOTP,
		}, nil
	}

	if !s.otps.Enabled() {
		slog.Error("live SMS OTP requires Redis; refusing to send")
		return SendOTPResponse{}, ErrOTPUnavailable
	}
	if _, ok := sms.NormalizeMobile(mobile); !ok {
		return SendOTPResponse{}, ErrInvalidMobile
	}
	// Per-number throttle protects the SMS budget; the per-IP middleware
	// limit alone is bypassed by rotating IPs.
	if err := s.otps.Throttle(ctx, mobile); err != nil {
		return SendOTPResponse{}, err
	}
	code, err := otp.Generate()
	if err != nil {
		return SendOTPResponse{}, err
	}
	if err := s.otps.Save(ctx, mobile, code); err != nil {
		return SendOTPResponse{}, err
	}
	if err := s.sendOTPSMS(ctx, mobile, code); err != nil {
		s.otps.Delete(ctx, mobile)
		slog.Error("otp sms failed", "error", err)
		return SendOTPResponse{}, ErrOTPUnavailable
	}
	return SendOTPResponse{Message: "OTP sent successfully"}, nil
}

// sendOTPSMS picks the login or registration template by whether the number
// already has an account.
func (s *Service) sendOTPSMS(ctx context.Context, mobile, code string) error {
	if s.sms == nil {
		return nil
	}
	req := sms.SendRequest{
		TemplateCode:  sms.TemplateAuthRegisterOTP,
		Mobile:        mobile,
		Values:        map[string]string{"otp": code, "valid_minutes": strconv.Itoa(int(otpTTL / time.Minute))},
		Sensitive:     []string{"otp"},
		ReferenceType: "AUTH_OTP",
	}
	user, err := s.repository.FindUserByMobile(ctx, mobile)
	switch {
	case err == nil:
		req.TemplateCode, req.UserID = sms.TemplateAuthLoginOTP, &user.ID
	case !errors.Is(err, ErrUserNotFound):
		return err
	}
	_, err = s.sms.Send(ctx, req)
	return err
}

// otpMobile keys OTPs by the normalized 10-digit number so "+91 98..." and
// "98..." share one OTP; anything else is used as typed.
func otpMobile(raw string) string {
	if m, ok := sms.NormalizeMobile(raw); ok {
		return m
	}
	return strings.TrimSpace(raw)
}

func (s *Service) VerifyOTP(ctx context.Context, req VerifyOTPRequest, client ClientContext) (AuthResponse, error) {
	// The fixed dev OTP is accepted only in mock mode.
	devCode := mockOTP
	if s.liveOTP() {
		devCode = ""
	}
	if !s.otps.Verify(ctx, otpMobile(req.Mobile), strings.TrimSpace(req.OTP), devCode) {
		return AuthResponse{}, ErrInvalidOTP
	}

	now := time.Now()
	isNewUser := false
	user, err := s.repository.FindUserByMobile(ctx, req.Mobile)
	if errors.Is(err, ErrUserNotFound) {
		isNewUser = true
		user, err = s.repository.CreateUser(ctx, req.Mobile)
		if err != nil {
			return AuthResponse{}, err
		}
		if err := s.repository.AssignDefaultRole(ctx, user.ID); err != nil {
			return AuthResponse{}, err
		}
		user.Roles, _ = s.repository.GetUserRoles(ctx, user.ID)
		s.raiseWelcome(user.ID, user.Mobile)
	} else if err != nil {
		return AuthResponse{}, err
	}

	if user.Status == "SUSPENDED" {
		return AuthResponse{}, &SuspendedError{
			Reason:      func() string { if user.SuspensionReason != nil { return *user.SuspensionReason }; return "" }(),
			SuspendedAt: user.SuspendedAt,
		}
	}

	if err := s.repository.UpdateLastLogin(ctx, user.ID, now); err != nil {
		return AuthResponse{}, err
	}

	sessionID, err := s.repository.CreateSession(ctx, CreateSessionInput{
		UserID:     user.ID,
		DeviceType: strings.ToUpper(strings.TrimSpace(req.DeviceType)),
		OSName:     strings.ToUpper(strings.TrimSpace(req.OSName)),
		AppVersion: req.AppVersion,
		IPAddress:  client.IPAddress,
		UserAgent:  client.UserAgent,
		LoginTime:  now,
	})
	if err != nil {
		return AuthResponse{}, err
	}

	accessToken, refreshToken, err := s.issueTokenPair(ctx, user, sessionID)
	if err != nil {
		return AuthResponse{}, err
	}
	if err := s.repository.StoreRefreshToken(ctx, sessionID, refreshToken); err != nil {
		return AuthResponse{}, err
	}

	activityJSON := mustJSON(map[string]any{
		"mobile":      user.Mobile,
		"device_type": req.DeviceType,
		"os_name":     req.OSName,
		"app_version": req.AppVersion,
	})
	if err := s.repository.CreateUserActivity(ctx, CreateActivityInput{
		UserID:    user.ID,
		SessionID: sessionID,
		Type:      activityLogin,
		DataJSON:  activityJSON,
		At:        now,
	}); err != nil {
		return AuthResponse{}, err
	}

	s.audit.LogSecurity(ctx, auditlog.SecurityEntry{
		UserID:      user.ID,
		EventType:   auditlog.SecurityEventLogin,
		Description: "User logged in via OTP",
		Metadata: map[string]any{
			"session_id":  sessionID,
			"device_type": req.DeviceType,
			"os_name":     req.OSName,
			"app_version": req.AppVersion,
			"is_new_user": isNewUser,
		},
		IPAddress:  client.IPAddress,
		DeviceInfo: client.UserAgent,
	})

	user.LastLoginAt = &now

	return AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		User:         *user,
		IsNewUser:    isNewUser,
	}, nil
}

// raiseWelcome fans the welcome out to every channel with an ACCOUNT_WELCOME
// template; it runs detached so a slow gateway never delays login.
func (s *Service) raiseWelcome(userID int64, mobile string) {
	if s.events == nil {
		return
	}
	s.events.NotifyAsync(messaging.Event{
		Code:          sms.TemplateAccountWelcome,
		UserID:        userID,
		Values:        map[string]string{"mobile": mobile},
		ReferenceType: "USER",
		ReferenceID:   &userID,
	})
}

func (s *Service) RefreshToken(ctx context.Context, req RefreshTokenRequest) (AuthResponse, error) {
	claims, err := s.jwt.VerifyRefreshToken(req.RefreshToken)
	if err != nil {
		return AuthResponse{}, ErrInvalidRefreshToken
	}

	userID, err := parseUserID(claims.UserID)
	if err != nil {
		return AuthResponse{}, err
	}

	session, err := s.repository.FindActiveSessionByToken(ctx, req.RefreshToken)
	if err != nil {
		return AuthResponse{}, err
	}
	if session.UserID != userID {
		return AuthResponse{}, ErrInvalidRefreshToken
	}

	user, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return AuthResponse{}, err
	}

	accessToken, refreshToken, err := s.issueTokenPair(ctx, user, session.ID)
	if err != nil {
		return AuthResponse{}, err
	}
	if err := s.repository.StoreRefreshToken(ctx, session.ID, refreshToken); err != nil {
		return AuthResponse{}, err
	}

	return AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		User:         *user,
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string, accessToken string) error {
	if refreshToken == "" && accessToken == "" {
		return ErrInvalidRefreshToken
	}

	if refreshToken != "" {
		session, err := s.repository.FindActiveSessionByToken(ctx, refreshToken)
		if err != nil {
			return err
		}
		if err := s.repository.LogoutSession(ctx, session.ID); err != nil {
			return err
		}
		s.blocklistJWT(ctx, refreshToken)
	}
	if accessToken != "" {
		s.blocklistJWT(ctx, accessToken)
	}
	return nil
}

func (s *Service) blocklistJWT(ctx context.Context, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	claims, err := s.jwt.Verify(token)
	if err != nil || claims.ExpiresAt == nil {
		return
	}
	redisutil.BlocklistToken(ctx, s.redis, slog.Default(), claims.ID, time.Until(claims.ExpiresAt.Time))
}

func (s *Service) Me(ctx context.Context, accessToken string) (User, error) {
	claims, err := s.jwt.VerifyAccessToken(accessToken)
	if err != nil {
		return User{}, ErrInvalidToken
	}

	userID, err := parseUserID(claims.UserID)
	if err != nil {
		return User{}, err
	}

	user, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return User{}, err
	}
	return *user, nil
}

func (s *Service) Workspaces(ctx context.Context, accessToken string) ([]Workspace, error) {
	claims, err := s.jwt.VerifyAccessToken(accessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	userID, err := parseUserID(claims.UserID)
	if err != nil {
		return nil, err
	}
	if workspaces, ok := s.getCachedWorkspaces(ctx, userID); ok {
		return workspaces, nil
	}
	workspaces, err := s.repository.GetWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.cacheWorkspaces(ctx, userID, workspaces)
	return workspaces, nil
}

func (s *Service) getCachedWorkspaces(ctx context.Context, userID int64) ([]Workspace, bool) {
	if s.redis == nil {
		return nil, false
	}
	data, err := s.redis.Get(ctx, redisutil.WorkspaceKey(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false
	}
	if err != nil {
		slog.Warn("redis workspace cache read failed", "user_id", userID, "error", err)
		return nil, false
	}
	var workspaces []Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		slog.Warn("redis workspace cache decode failed", "user_id", userID, "error", err)
		return nil, false
	}
	return workspaces, true
}

func (s *Service) cacheWorkspaces(ctx context.Context, userID int64, workspaces []Workspace) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(workspaces)
	if err != nil {
		slog.Warn("redis workspace cache encode failed", "user_id", userID, "error", err)
		return
	}
	if err := s.redis.Set(ctx, redisutil.WorkspaceKey(userID), data, time.Duration(redisutil.WorkspaceTTLSeconds)*time.Second).Err(); err != nil {
		slog.Warn("redis workspace cache write failed", "user_id", userID, "error", err)
	}
}

func (s *Service) OwnerDashboard(ctx context.Context, accessToken string) (*OwnerDashboard, error) {
	claims, err := s.jwt.VerifyAccessToken(accessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	userID, err := parseUserID(claims.UserID)
	if err != nil {
		return nil, err
	}
	if !hasAnyRole(claims.Roles, "NURSERY_OWNER") {
		return nil, ErrForbidden
	}
	return s.repository.GetOwnerDashboard(ctx, userID)
}

func hasAnyRole(roles []string, allowed ...string) bool {
	for _, role := range roles {
		for _, item := range allowed {
			if strings.EqualFold(role, item) {
				return true
			}
		}
	}
	return false
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
