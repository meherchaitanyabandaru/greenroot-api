package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
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
}

type ClientContext struct {
	IPAddress string
	UserAgent string
}

func NewService(repository Repository, jwt *jwtplatform.Service) *Service {
	return &Service{repository: repository, jwt: jwt}
}

func (s *Service) SendOTP(ctx context.Context, req SendOTPRequest) (SendOTPResponse, error) {
	return SendOTPResponse{
		Message: "OTP sent successfully",
		MockOTP: mockOTP,
	}, nil
}

func (s *Service) VerifyOTP(ctx context.Context, req VerifyOTPRequest, client ClientContext) (AuthResponse, error) {
	if req.OTP != mockOTP {
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

	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: "users",
		RecordID:  user.ID,
		Action:    "UPDATE",
		ChangedBy: user.ID,
		SourceIP:  client.IPAddress,
		UserAgent: client.UserAgent,
		NewJSON: mustJSON(map[string]any{
			"event":         activityLogin,
			"last_login_at": now.Format(time.RFC3339),
			"session_id":    sessionID,
		}),
		At: now,
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
