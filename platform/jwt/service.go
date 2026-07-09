package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type Claims struct {
	UserID    string   `json:"user_id"`
	SessionID string   `json:"session_id,omitempty"`
	Mobile    string   `json:"mobile,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	TokenType string   `json:"token_type"`

	// Rich context — embedded at issue time, zero DB cost on every request.
	UserStatus      string `json:"user_status,omitempty"`      // ACTIVE | SUSPENDED | DELETED
	NurseryID       int64  `json:"nursery_id,omitempty"`       // 0 if not nursery-affiliated
	NurseryStatus   string `json:"nursery_status,omitempty"`   // ACTIVE | SUSPENDED | PENDING_APPROVAL
	SubTier         string `json:"sub_tier,omitempty"`         // TRIAL | GROWTH | ENTERPRISE | ""
	SubExpiresEpoch int64  `json:"sub_exp,omitempty"`          // Unix epoch of end_date; 0 = no expiry

	jwt.RegisteredClaims
}

// TokenContext is fetched once at login/refresh and embedded in the JWT.
type TokenContext struct {
	UserStatus      string
	NurseryID       int64
	NurseryStatus   string
	SubTier         string
	SubExpiresEpoch int64
}

type Service struct {
	cfg config.JWTConfig
}

func NewService(cfg config.JWTConfig) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) IssueAccessToken(userID, sessionID, mobile string, roles []string, tc TokenContext) (string, error) {
	return s.issue(TokenTypeAccess, userID, sessionID, mobile, roles, tc, s.cfg.AccessTTL)
}

func (s *Service) IssueRefreshToken(userID, sessionID, mobile string, roles []string, tc TokenContext) (string, error) {
	return s.issue(TokenTypeRefresh, userID, sessionID, mobile, roles, tc, s.cfg.RefreshTTL)
}

func (s *Service) Verify(tokenValue string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *Service) VerifyAccessToken(tokenValue string) (*Claims, error) {
	claims, err := s.Verify(tokenValue)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, errors.New("token is not an access token")
	}
	return claims, nil
}

func (s *Service) VerifyRefreshToken(tokenValue string) (*Claims, error) {
	claims, err := s.Verify(tokenValue)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeRefresh {
		return nil, errors.New("token is not a refresh token")
	}
	return claims, nil
}

func (s *Service) issue(tokenType, userID, sessionID, mobile string, roles []string, tc TokenContext, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:          userID,
		SessionID:       sessionID,
		Mobile:          mobile,
		Roles:           roles,
		TokenType:       tokenType,
		UserStatus:      tc.UserStatus,
		NurseryID:       tc.NurseryID,
		NurseryStatus:   tc.NurseryStatus,
		SubTier:         tc.SubTier,
		SubExpiresEpoch: tc.SubExpiresEpoch,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.cfg.Issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.Secret))
}
