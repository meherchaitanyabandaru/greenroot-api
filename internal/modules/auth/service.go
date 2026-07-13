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
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
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
	ErrForbidden           = errors.New("forbidden")
)

type Service struct {
	repository Repository
	jwt        *jwtplatform.Service
	audit      *auditlog.Service
	redis      redis.Cmdable
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
	return &Service{repository: repository, jwt: jwt, audit: audit, redis: rdb}
}

func (s *Service) SendOTP(ctx context.Context, req SendOTPRequest) (SendOTPResponse, error) {
	s.storeOTP(ctx, strings.TrimSpace(req.Mobile), mockOTP)
	return SendOTPResponse{
		Message: "OTP sent successfully",
		MockOTP: mockOTP,
	}, nil
}

func (s *Service) VerifyOTP(ctx context.Context, req VerifyOTPRequest, client ClientContext) (AuthResponse, error) {
	if !s.consumeOTP(ctx, strings.TrimSpace(req.Mobile), strings.TrimSpace(req.OTP)) {
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
	} else if err != nil {
		return AuthResponse{}, err
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

func (s *Service) storeOTP(ctx context.Context, mobile string, code string) {
	if s.redis == nil || mobile == "" {
		return
	}
	if err := s.redis.Set(ctx, redisutil.KeyOTP+mobile, code, otpTTL).Err(); err != nil {
		slog.Warn("redis otp set failed; falling back to dev otp verification", "mobile", mobile, "error", err)
	}
}

func (s *Service) consumeOTP(ctx context.Context, mobile string, code string) bool {
	if s.redis == nil {
		return code == mockOTP
	}
	key := redisutil.KeyOTP + mobile
	stored, err := s.redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false
	}
	if err != nil {
		slog.Warn("redis otp get failed; falling back to dev otp verification", "mobile", mobile, "error", err)
		return code == mockOTP
	}
	if stored != code {
		return false
	}
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		slog.Warn("redis otp delete failed", "mobile", mobile, "error", err)
	}
	return true
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

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidRefreshToken
	}

	session, err := s.repository.FindActiveSessionByToken(ctx, token)
	if err != nil {
		return err
	}
	return s.repository.LogoutSession(ctx, session.ID)
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
	return s.repository.GetWorkspaces(ctx, userID)
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
