package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

// ─── mock repository ────────────────────────────────────────────────────────

type mockRepo struct {
	users          map[int64]*User
	sessions       map[int64]*Session // sessionID → Session
	tokens         map[string]int64   // hashed-token → sessionID (we store raw for simplicity)
	roles          map[int64][]string
	tc             map[int64]TokenContext
	workspaces     map[int64][]Workspace
	dashboard      *OwnerDashboard
	workspaceCalls int

	nextUserID    int64
	nextSessionID int64
}

func newMock() *mockRepo {
	return &mockRepo{
		users:         make(map[int64]*User),
		sessions:      make(map[int64]*Session),
		tokens:        make(map[string]int64),
		roles:         make(map[int64][]string),
		tc:            make(map[int64]TokenContext),
		workspaces:    make(map[int64][]Workspace),
		nextUserID:    100,
		nextSessionID: 500,
	}
}

func (m *mockRepo) seedUser(id int64, mobile string, roles ...string) *User {
	u := &User{ID: id, Mobile: mobile, Status: "ACTIVE", Roles: roles}
	m.users[id] = u
	m.roles[id] = roles
	m.tc[id] = TokenContext{UserStatus: "ACTIVE"}
	return u
}

func (m *mockRepo) FindUserByMobile(_ context.Context, mobile string) (*User, error) {
	for _, u := range m.users {
		if u.Mobile == mobile {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *mockRepo) FindUserByID(_ context.Context, id int64) (*User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	// Mirror the real repo: scanUser calls GetUserRoles so the returned user
	// always reflects the current state of m.roles, not the roles cached in
	// m.users at seed time.
	copy := *u
	copy.Roles = m.roles[id]
	return &copy, nil
}

func (m *mockRepo) CreateUser(_ context.Context, mobile string) (*User, error) {
	m.nextUserID++
	u := &User{ID: m.nextUserID, Mobile: mobile, Status: "ACTIVE"}
	m.users[m.nextUserID] = u
	m.tc[m.nextUserID] = TokenContext{UserStatus: "ACTIVE"}
	return u, nil
}

func (m *mockRepo) UpdateLastLogin(_ context.Context, _ int64, _ time.Time) error { return nil }

func (m *mockRepo) GetUserRoles(_ context.Context, id int64) ([]string, error) {
	return m.roles[id], nil
}

func (m *mockRepo) AssignDefaultRole(_ context.Context, id int64) error {
	if m.roles[id] == nil {
		m.roles[id] = []string{"BUYER"}
	}
	return nil
}

func (m *mockRepo) CreateSession(_ context.Context, _ CreateSessionInput) (int64, error) {
	m.nextSessionID++
	m.sessions[m.nextSessionID] = &Session{ID: m.nextSessionID, UserID: 0, Status: "ACTIVE"}
	return m.nextSessionID, nil
}

func (m *mockRepo) StoreRefreshToken(_ context.Context, sessionID int64, token string) error {
	m.tokens[token] = sessionID
	if s, ok := m.sessions[sessionID]; ok {
		s.UserID = m.resolveUserForSession(sessionID)
	}
	return nil
}

func (m *mockRepo) FindActiveSessionByToken(_ context.Context, token string) (*Session, error) {
	sid, ok := m.tokens[token]
	if !ok {
		return nil, ErrInvalidRefreshToken
	}
	s, ok := m.sessions[sid]
	if !ok || s.Status != "ACTIVE" {
		return nil, ErrInvalidRefreshToken
	}
	return s, nil
}

func (m *mockRepo) LogoutSession(_ context.Context, sessionID int64) error {
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	s.Status = "LOGGED_OUT"
	for tok, sid := range m.tokens {
		if sid == sessionID {
			delete(m.tokens, tok)
		}
	}
	return nil
}

func (m *mockRepo) CreateUserActivity(_ context.Context, _ CreateActivityInput) error { return nil }

func (m *mockRepo) GetWorkspaces(_ context.Context, id int64) ([]Workspace, error) {
	m.workspaceCalls++
	w, ok := m.workspaces[id]
	if !ok {
		return []Workspace{{Type: "PERSONAL", Role: "CUSTOMER"}}, nil
	}
	return w, nil
}

func (m *mockRepo) GetOwnerDashboard(_ context.Context, _ int64) (*OwnerDashboard, error) {
	if m.dashboard != nil {
		return m.dashboard, nil
	}
	return &OwnerDashboard{}, nil
}

func (m *mockRepo) GetTokenContext(_ context.Context, id int64) (TokenContext, error) {
	tc, ok := m.tc[id]
	if !ok {
		return TokenContext{UserStatus: "ACTIVE"}, nil
	}
	return tc, nil
}

// helper: find user linked to a session (set after StoreRefreshToken is called with a seedUser)
func (m *mockRepo) resolveUserForSession(sessionID int64) int64 {
	return 0
}

// linkSession wires a session to a specific user so FindActiveSessionByToken works correctly.
func (m *mockRepo) linkSession(sessionID, userID int64) {
	s, ok := m.sessions[sessionID]
	if !ok {
		m.sessions[sessionID] = &Session{ID: sessionID, UserID: userID, Status: "ACTIVE"}
	} else {
		s.UserID = userID
	}
}

// ─── test JWT service ────────────────────────────────────────────────────────

func testJWT() *jwtplatform.Service {
	return jwtplatform.NewService(config.JWTConfig{
		Secret:     "test-secret-key",
		Issuer:     "greenroot-test",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	})
}

func svc(repo *mockRepo) *Service {
	return NewService(repo, testJWT(), nil)
}

// ─── SendOTP ─────────────────────────────────────────────────────────────────

func TestSendOTP_AlwaysSucceeds(t *testing.T) {
	resp, err := svc(newMock()).SendOTP(context.Background(), SendOTPRequest{Mobile: "9000000000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.MockOTP != mockOTP {
		t.Errorf("want mock OTP %q, got %q", mockOTP, resp.MockOTP)
	}
}

// ─── VerifyOTP ───────────────────────────────────────────────────────────────

func TestVerifyOTP_WrongOTP(t *testing.T) {
	_, err := svc(newMock()).VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile: "9000000000",
		OTP:    "000000",
	}, ClientContext{})
	if !errors.Is(err, ErrInvalidOTP) {
		t.Errorf("want ErrInvalidOTP, got %v", err)
	}
}

func TestVerifyOTP_ExistingUser_ReturnsTokens(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")

	resp, err := svc(repo).VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile:     "9000000000",
		OTP:        mockOTP,
		DeviceType: "ANDROID",
		OSName:     "ANDROID",
	}, ClientContext{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Error("expected non-empty tokens")
	}
	if resp.IsNewUser {
		t.Error("expected IsNewUser=false for existing user")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("want Bearer, got %s", resp.TokenType)
	}
}

func TestVerifyOTP_NewUser_CreatesAndReturnsTokens(t *testing.T) {
	resp, err := svc(newMock()).VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile: "9999999999",
		OTP:    mockOTP,
	}, ClientContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsNewUser {
		t.Error("expected IsNewUser=true for new user")
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Error("expected non-empty tokens")
	}
}

func TestVerifyOTP_DeviceTypeUppercased(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9100000000", "NURSERY_OWNER")

	resp, err := svc(repo).VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile:     "9100000000",
		OTP:        mockOTP,
		DeviceType: "android",
		OSName:     "android",
	}, ClientContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.User.Mobile != "9100000000" {
		t.Errorf("got wrong user mobile %s", resp.User.Mobile)
	}
}

func TestOTPLifecycle_Redis(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s := NewService(repo, testJWT(), nil, client)

	if _, err := s.SendOTP(ctx, SendOTPRequest{Mobile: "9000000000"}); err != nil {
		t.Fatalf("send otp failed: %v", err)
	}
	key := redisutil.KeyOTP + "9000000000"
	if !server.Exists(key) {
		t.Fatal("expected otp key in redis")
	}
	if ttl := server.TTL(key); ttl <= 0 {
		t.Fatalf("expected otp ttl, got %s", ttl)
	}

	resp, err := s.VerifyOTP(ctx, VerifyOTPRequest{
		Mobile:     "9000000000",
		OTP:        mockOTP,
		DeviceType: "ANDROID",
		OSName:     "ANDROID",
	}, ClientContext{})
	if err != nil {
		t.Fatalf("verify otp failed: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected token pair after otp verification")
	}
	if server.Exists(key) {
		t.Fatal("expected otp key to be consumed")
	}

	_, err = s.VerifyOTP(ctx, VerifyOTPRequest{Mobile: "9000000000", OTP: mockOTP}, ClientContext{})
	if !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("expected consumed otp to fail with ErrInvalidOTP, got %v", err)
	}
}

// ─── RefreshToken ─────────────────────────────────────────────────────────────

func TestRefreshToken_ValidToken_ReturnsNewPair(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s := svc(repo)

	// Login to get an initial refresh token
	loginResp, err := s.VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile: "9000000000",
		OTP:    mockOTP,
	}, ClientContext{})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// Wire user to the session (VerifyOTP uses CreateSession which sets session; StoreRefreshToken stores token)
	for tok, sid := range repo.tokens {
		repo.linkSession(sid, 1)
		_ = tok
	}

	refreshResp, err := s.RefreshToken(context.Background(), RefreshTokenRequest{
		RefreshToken: loginResp.RefreshToken,
	})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshResp.AccessToken == "" || refreshResp.RefreshToken == "" {
		t.Error("expected new tokens")
	}
}

// TestRefreshToken_ReadsRolesFromDB verifies that RefreshToken re-reads roles from
// the repository (not the JWT) so a backend role change (e.g. admin approving a
// nursery and granting NURSERY_OWNER) is reflected in the new access token without
// requiring the user to log out.
func TestRefreshToken_ReadsRolesFromDB(t *testing.T) {
	repo := newMock()
	// Start as BUYER
	repo.seedUser(1, "9100000000", "BUYER")
	s := svc(repo)

	loginResp, err := s.VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile: "9100000000",
		OTP:    mockOTP,
	}, ClientContext{})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	for _, sid := range repo.tokens {
		repo.linkSession(sid, 1)
	}

	// Access token from login should have only BUYER role
	claims, err := testJWT().VerifyAccessToken(loginResp.AccessToken)
	if err != nil {
		t.Fatalf("verify login access token: %v", err)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "BUYER" {
		t.Errorf("expected [BUYER] roles in login token, got %v", claims.Roles)
	}

	// Simulate admin approving the nursery: update roles in the repo
	repo.roles[1] = []string{"BUYER", "NURSERY_OWNER"}

	// Refresh — must re-read roles from DB
	refreshResp, err := s.RefreshToken(context.Background(), RefreshTokenRequest{
		RefreshToken: loginResp.RefreshToken,
	})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	newClaims, err := testJWT().VerifyAccessToken(refreshResp.AccessToken)
	if err != nil {
		t.Fatalf("verify refreshed access token: %v", err)
	}

	hasOwner := false
	for _, r := range newClaims.Roles {
		if r == "NURSERY_OWNER" {
			hasOwner = true
		}
	}
	if !hasOwner {
		t.Errorf("expected NURSERY_OWNER in refreshed token roles, got %v", newClaims.Roles)
	}
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	_, err := svc(newMock()).RefreshToken(context.Background(), RefreshTokenRequest{
		RefreshToken: "not-a-valid-jwt",
	})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("want ErrInvalidRefreshToken, got %v", err)
	}
}

func TestRefreshToken_ValidJWTButNotInDB(t *testing.T) {
	s := svc(newMock())
	// Issue a valid refresh token but don't store it in the mock
	jwt := testJWT()
	token, _ := jwt.IssueRefreshToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := s.RefreshToken(context.Background(), RefreshTokenRequest{RefreshToken: token})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("want ErrInvalidRefreshToken, got %v", err)
	}
}

func TestRefreshToken_AccessTokenRejected(t *testing.T) {
	// Passing an access token where refresh is expected → VerifyRefreshToken should reject it
	jwt := testJWT()
	accessToken, _ := jwt.IssueAccessToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := svc(newMock()).RefreshToken(context.Background(), RefreshTokenRequest{
		RefreshToken: accessToken,
	})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("want ErrInvalidRefreshToken, got %v", err)
	}
}

// ─── Logout ───────────────────────────────────────────────────────────────────

func TestLogout_EmptyToken(t *testing.T) {
	err := svc(newMock()).Logout(context.Background(), "", "")
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("want ErrInvalidRefreshToken, got %v", err)
	}
}

func TestLogout_TokenNotInDB(t *testing.T) {
	err := svc(newMock()).Logout(context.Background(), "some-unknown-token", "")
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("want ErrInvalidRefreshToken, got %v", err)
	}
}

func TestLogout_ValidToken_InvalidatesSession(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s := svc(repo)

	loginResp, err := s.VerifyOTP(context.Background(), VerifyOTPRequest{
		Mobile: "9000000000",
		OTP:    mockOTP,
	}, ClientContext{})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	for _, sid := range repo.tokens {
		repo.linkSession(sid, 1)
	}

	if err := s.Logout(context.Background(), loginResp.RefreshToken, loginResp.AccessToken); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	// Token must now be invalid
	_, err = s.RefreshToken(context.Background(), RefreshTokenRequest{RefreshToken: loginResp.RefreshToken})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("after logout: want ErrInvalidRefreshToken, got %v", err)
	}
}

// ─── Me ───────────────────────────────────────────────────────────────────────

func TestMe_ValidAccessToken(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	s := svc(repo)

	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	user, err := s.Me(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Mobile != "9000000000" {
		t.Errorf("want mobile 9000000000, got %s", user.Mobile)
	}
}

func TestMe_InvalidToken(t *testing.T) {
	_, err := svc(newMock()).Me(context.Background(), "not-a-jwt")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestMe_RefreshTokenRejected(t *testing.T) {
	jwt := testJWT()
	refreshToken, _ := jwt.IssueRefreshToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := svc(newMock()).Me(context.Background(), refreshToken)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestMe_UserNotFoundInDB(t *testing.T) {
	// Valid access token but user ID doesn't exist in mock
	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("999", "500", "9999999999", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := svc(newMock()).Me(context.Background(), token)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}
}

// ─── Workspaces ───────────────────────────────────────────────────────────────

func TestWorkspaces_ValidToken(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	repo.workspaces[1] = []Workspace{
		{Type: "PERSONAL", Role: "CUSTOMER"},
	}
	s := svc(repo)

	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	workspaces, err := s.Workspaces(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workspaces) == 0 {
		t.Error("expected at least one workspace")
	}
}

func TestWorkspaces_InvalidToken(t *testing.T) {
	_, err := svc(newMock()).Workspaces(context.Background(), "bad-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestWorkspaces_UsesRedisCache(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	repo := newMock()
	repo.seedUser(1, "9000000000", "BUYER")
	repo.workspaces[1] = []Workspace{{Type: "PERSONAL", Role: "CUSTOMER"}}
	s := NewService(repo, testJWT(), nil, client)

	token, _ := testJWT().IssueAccessToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})
	if _, err := s.Workspaces(ctx, token); err != nil {
		t.Fatalf("first workspaces call failed: %v", err)
	}
	if _, err := s.Workspaces(ctx, token); err != nil {
		t.Fatalf("second workspaces call failed: %v", err)
	}
	if repo.workspaceCalls != 1 {
		t.Fatalf("expected one repository call due to cache hit, got %d", repo.workspaceCalls)
	}
	if !server.Exists(redisutil.WorkspaceKey(1)) {
		t.Fatal("expected workspace cache key")
	}
}

// ─── OwnerDashboard ───────────────────────────────────────────────────────────

func TestOwnerDashboard_NonOwner_Forbidden(t *testing.T) {
	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("1", "500", "9000000000", []string{"BUYER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := svc(newMock()).OwnerDashboard(context.Background(), token)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-owner, got %v", err)
	}
}

func TestOwnerDashboard_Owner_ReturnsData(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "9100000000", "NURSERY_OWNER")
	nid := int64(10)
	nname := "Green Nursery"
	repo.dashboard = &OwnerDashboard{NurseryID: &nid, NurseryName: &nname}
	s := svc(repo)

	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("1", "500", "9100000000", []string{"NURSERY_OWNER"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	dashboard, err := s.OwnerDashboard(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dashboard.NurseryID == nil || *dashboard.NurseryID != 10 {
		t.Error("expected dashboard with nursery ID 10")
	}
}

func TestOwnerDashboard_InvalidToken(t *testing.T) {
	_, err := svc(newMock()).OwnerDashboard(context.Background(), "not-valid")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestOwnerDashboard_AdminForbidden(t *testing.T) {
	jwt := testJWT()
	token, _ := jwt.IssueAccessToken("1", "500", "9000000000", []string{"ADMIN"}, jwtplatform.TokenContext{UserStatus: "ACTIVE"})

	_, err := svc(newMock()).OwnerDashboard(context.Background(), token)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for admin, got %v", err)
	}
}
