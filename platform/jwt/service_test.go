package jwt

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

var testCfg = config.JWTConfig{Secret: strings.Repeat("k", 40), Issuer: "greenroot-api", AccessTTL: time.Minute, RefreshTTL: time.Hour}

func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIssueAndVerify(t *testing.T) {
	s := NewService(testCfg)
	access, err := s.IssueAccessToken("42", "7", "9000000000", []string{"BUYER"}, TokenContext{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.VerifyAccessToken(access)
	if err != nil || c.UserID != "42" || c.Issuer != "greenroot-api" {
		t.Fatalf("verify access: %+v %v", c, err)
	}
	if _, err := s.VerifyRefreshToken(access); err == nil {
		t.Fatal("an access token must not pass as a refresh token")
	}
	refresh, _ := s.IssueRefreshToken("42", "7", "", nil, TokenContext{})
	if _, err := s.VerifyAccessToken(refresh); err == nil {
		t.Fatal("a refresh token must not pass as an access token")
	}
}

func TestVerifyRejectsForgeries(t *testing.T) {
	s := NewService(testCfg)
	exp := time.Now().Add(time.Minute).Unix()
	base := func() jwt.MapClaims {
		return jwt.MapClaims{"user_id": "1", "roles": []string{"ADMIN"}, "token_type": "access", "iss": "greenroot-api", "exp": exp}
	}
	cases := map[string]string{
		"wrong secret": sign(t, jwt.SigningMethodHS256, []byte("local-dev-change-me"), base()),
		"other issuer": func() string {
			c := base()
			c["iss"] = "someone-else"
			return sign(t, jwt.SigningMethodHS256, []byte(testCfg.Secret), c)
		}(),
		"no expiry": func() string {
			c := base()
			delete(c, "exp")
			return sign(t, jwt.SigningMethodHS256, []byte(testCfg.Secret), c)
		}(),
		"expired": func() string {
			c := base()
			c["exp"] = time.Now().Add(-time.Minute).Unix()
			return sign(t, jwt.SigningMethodHS256, []byte(testCfg.Secret), c)
		}(),
		"HS512":    sign(t, jwt.SigningMethodHS512, []byte(testCfg.Secret), base()),
		"alg none": sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, base()),
	}
	for name, token := range cases {
		if _, err := s.VerifyAccessToken(token); err == nil {
			t.Errorf("%s: forged token accepted", name)
		}
	}
	if _, err := s.VerifyAccessToken(sign(t, jwt.SigningMethodHS256, []byte(testCfg.Secret), base())); err != nil {
		t.Fatalf("control token rejected: %v", err)
	}
}
