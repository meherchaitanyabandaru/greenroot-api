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
	jwt.RegisteredClaims
}

type Service struct {
	cfg config.JWTConfig
}

func NewService(cfg config.JWTConfig) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) IssueAccessToken(userID string, sessionID string, mobile string, roles []string) (string, error) {
	return s.issue(TokenTypeAccess, userID, sessionID, mobile, roles, s.cfg.AccessTTL)
}

func (s *Service) IssueRefreshToken(userID string, sessionID string, mobile string, roles []string) (string, error) {
	return s.issue(TokenTypeRefresh, userID, sessionID, mobile, roles, s.cfg.RefreshTTL)
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

func (s *Service) issue(tokenType string, userID string, sessionID string, mobile string, roles []string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:    userID,
		SessionID: sessionID,
		Mobile:    mobile,
		Roles:     roles,
		TokenType: tokenType,
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
