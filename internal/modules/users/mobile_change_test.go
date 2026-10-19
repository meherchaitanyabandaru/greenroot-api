package users

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/redis/go-redis/v9"
)

type fakeSMS struct {
	live bool
	reqs []sms.SendRequest
}

func (f *fakeSMS) Live() bool { return f.live }
func (f *fakeSMS) Send(_ context.Context, req sms.SendRequest) (sms.SendResult, error) {
	f.reqs = append(f.reqs, req)
	return sms.SendResult{}, nil
}

func mobileService(t *testing.T, live bool) (*Service, *mockRepo, *fakeSMS) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	repo := newMock()
	repo.seedUser(1, "Ravi", "9300000000")
	repo.seedUser(2, "Sita", "9300000001")
	sender := &fakeSMS{live: live}
	return NewService(repo, nil, nil, client).WithSMS(sender), repo, sender
}

func TestMobileChange_SendsToTheNewNumberAndSwitches(t *testing.T) {
	ctx := context.Background()
	s, repo, sender := mobileService(t, false)
	resp, err := s.SendMobileCode(ctx, buyer, MobileCodeRequest{Mobile: "+91 98765 43210"})
	if err != nil || resp.DeliveredTo != "9876543210" || resp.MockCode != mobileDevCode {
		t.Fatalf("send: %+v %v", resp, err)
	}
	if sender.reqs[0].Mobile != "9876543210" {
		t.Fatalf("code must go to the NEW number, got %q", sender.reqs[0].Mobile)
	}
	if _, err := s.VerifyMobile(ctx, buyer, VerifyMobileRequest{Mobile: "9876543210", Code: "000000"}); !errors.Is(err, ErrInvalidMobileCode) {
		t.Fatalf("wrong code: %v", err)
	}
	user, err := s.VerifyMobile(ctx, buyer, VerifyMobileRequest{Mobile: "9876543210", Code: mobileDevCode})
	if err != nil || user.Mobile != "9876543210" || repo.users[1].Mobile != "9876543210" {
		t.Fatalf("verify: %+v %v", user, err)
	}
}

func TestMobileChange_Refusals(t *testing.T) {
	ctx := context.Background()
	s, _, sender := mobileService(t, false)
	for raw, want := range map[string]error{
		"12345":         ErrInvalidMobile,
		"5876543210":    ErrInvalidMobile,
		"9300000000":    ErrSameMobile,
		"9300000001":    ErrMobileTaken,
		"+919300000001": ErrMobileTaken,
	} {
		if _, err := s.SendMobileCode(ctx, buyer, MobileCodeRequest{Mobile: raw}); !errors.Is(err, want) {
			t.Errorf("%s: want %v, got %v", raw, want, err)
		}
	}
	if len(sender.reqs) != 0 {
		t.Fatalf("no SMS may be sent for a refused number, sent %d", len(sender.reqs))
	}
}

func TestMobileChange_LiveModeHidesTheCode(t *testing.T) {
	s, _, _ := mobileService(t, true)
	resp, err := s.SendMobileCode(context.Background(), buyer, MobileCodeRequest{Mobile: "9876543210"})
	if err != nil || resp.MockCode != "" {
		t.Fatalf("live mode must not return the code: %+v %v", resp, err)
	}
	if _, err := s.VerifyMobile(context.Background(), buyer, VerifyMobileRequest{Mobile: "9876543210", Code: mobileDevCode}); !errors.Is(err, ErrInvalidMobileCode) {
		t.Fatalf("the dev code must not work live: %v", err)
	}
}
