package users

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/email"
	"github.com/redis/go-redis/v9"
)

type fakeEmail struct {
	live bool
	err  error
	reqs []email.SendRequest
}

func (f *fakeEmail) Live() bool { return f.live }
func (f *fakeEmail) Send(_ context.Context, req email.SendRequest) (email.SendResult, error) {
	f.reqs = append(f.reqs, req)
	return email.SendResult{Status: "SENT"}, f.err
}

var buyer = ActorContext{UserID: 1, Roles: []string{"BUYER"}}

func emailService(t *testing.T, live bool) (*Service, *mockRepo, *fakeEmail, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	repo := newMock()
	repo.seedUser(1, "Ravi", "9300000000")
	sender := &fakeEmail{live: live}
	return NewService(repo, nil, nil, client).WithEmail(sender), repo, sender, server
}

func TestEmailVerification_MockMode(t *testing.T) {
	s, repo, sender, _ := emailService(t, false)
	resp, err := s.SendEmailCode(context.Background(), buyer, EmailCodeRequest{Email: " Ravi@Example.IN "})
	if err != nil || resp.MockCode != emailDevCode || resp.DeliveredTo != "ravi@example.in" {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if req := sender.reqs[0]; req.TemplateCode != email.TemplateEmailVerifyOTP || req.To != "ravi@example.in" || req.Sensitive[0] != "otp" {
		t.Fatalf("send request = %+v", req)
	}
	user, err := s.VerifyEmail(context.Background(), buyer, VerifyEmailRequest{Email: "ravi@example.in", Code: emailDevCode})
	if err != nil || !user.EmailVerified || *repo.users[1].Email != "ravi@example.in" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
}

func TestEmailVerification_LiveMode(t *testing.T) {
	ctx := context.Background()
	s, repo, sender, _ := emailService(t, true)
	resp, err := s.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "ravi@example.in"})
	if err != nil || resp.MockCode != "" {
		t.Fatalf("live mode must not return the code: %+v %v", resp, err)
	}
	code := sender.reqs[0].Values["otp"]
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatalf("code = %q", code)
	}
	if code != emailDevCode {
		if _, err := s.VerifyEmail(ctx, buyer, VerifyEmailRequest{Email: "ravi@example.in", Code: emailDevCode}); !errors.Is(err, ErrInvalidEmailCode) {
			t.Fatalf("dev code must not work live, got %v", err)
		}
	}
	// The code is bound to the address it was sent to.
	if _, err := s.VerifyEmail(ctx, buyer, VerifyEmailRequest{Email: "other@example.in", Code: code}); !errors.Is(err, ErrInvalidEmailCode) {
		t.Fatalf("code must not verify a different address, got %v", err)
	}
	// ...and to the user who asked for it.
	other := ActorContext{UserID: 2, Roles: []string{"BUYER"}}
	repo.seedUser(2, "Other", "9300000001")
	if _, err := s.VerifyEmail(ctx, other, VerifyEmailRequest{Email: "ravi@example.in", Code: code}); !errors.Is(err, ErrInvalidEmailCode) {
		t.Fatalf("code must not verify another account, got %v", err)
	}
	user, err := s.VerifyEmail(ctx, buyer, VerifyEmailRequest{Email: "ravi@example.in", Code: code})
	if err != nil || !user.EmailVerified {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if _, err := s.VerifyEmail(ctx, buyer, VerifyEmailRequest{Email: "ravi@example.in", Code: code}); !errors.Is(err, ErrInvalidEmailCode) {
		t.Fatalf("code must be single-use, got %v", err)
	}
	if _, err := s.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "ravi@example.in"}); !errors.Is(err, ErrEmailCooldown) {
		t.Fatalf("immediate resend must hit cooldown, got %v", err)
	}
}

func TestEmailVerification_Rejections(t *testing.T) {
	ctx := context.Background()
	s, repo, sender, server := emailService(t, true)

	if _, err := s.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "not-an-email"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want invalid input, got %v", err)
	}
	taken := "taken@example.in"
	repo.seedUser(2, "Other", "9300000001").Email = &taken
	if _, err := s.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "TAKEN@example.in"}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("want email taken, got %v", err)
	}

	sender.err = errors.New("smtp down")
	if _, err := s.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "new@example.in"}); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("send failure must surface, got %v", err)
	}
	if server.Exists(emailOTPPrefix + emailCodeID(1, "new@example.in")) {
		t.Fatal("undelivered code must not stay valid")
	}

	noRedis := NewService(newMock(), nil, nil).WithEmail(&fakeEmail{live: true})
	if _, err := noRedis.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "new@example.in"}); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("live email without Redis must fail, got %v", err)
	}
	disabled := NewService(newMock(), nil, nil)
	if _, err := disabled.SendEmailCode(ctx, buyer, EmailCodeRequest{Email: "a@b.in"}); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("no email module must fail cleanly, got %v", err)
	}
}

func TestUpdateMe_NormalizesEmail(t *testing.T) {
	repo := newMock()
	repo.seedUser(1, "Ravi", "9300000000")
	s := svc(repo)
	mixed := " Ravi@Example.IN "
	user, err := s.UpdateMe(context.Background(), buyer, UpdateProfileRequest{FirstName: "Ravi", Email: &mixed})
	if err != nil || user.Email == nil || *user.Email != "ravi@example.in" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	bad := "nope"
	if _, err := s.UpdateMe(context.Background(), buyer, UpdateProfileRequest{FirstName: "Ravi", Email: &bad}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want invalid input, got %v", err)
	}
}

func TestUpdateMe_OmittedEmailKeepsAddress(t *testing.T) {
	repo := newMock()
	verified := "ravi@example.in"
	u := repo.seedUser(1, "Ravi", "9300000000")
	u.Email, u.EmailVerified = &verified, true
	user, err := svc(repo).UpdateMe(context.Background(), buyer, UpdateProfileRequest{FirstName: "Ravi K"})
	if err != nil || user.Email == nil || *user.Email != verified || !user.EmailVerified {
		t.Fatalf("saving a profile without email must keep the verified address: %+v %v", user, err)
	}
	empty := "  "
	user, err = svc(repo).UpdateMe(context.Background(), buyer, UpdateProfileRequest{FirstName: "Ravi K", Email: &empty})
	if err != nil || user.Email != nil {
		t.Fatalf("blank email must clear it: %+v %v", user, err)
	}
}
