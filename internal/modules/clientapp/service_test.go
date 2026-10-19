package clientapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

type fakeRepo struct {
	stored []CrashReport
	listed ListCrashReportsRequest
}

func (f *fakeRepo) InsertCrashReport(_ context.Context, c CrashReport) (int64, error) {
	f.stored = append(f.stored, c)
	return int64(len(f.stored)), nil
}

func (f *fakeRepo) ListCrashReports(_ context.Context, in ListCrashReportsRequest) ([]CrashReport, int64, error) {
	f.listed = in
	return f.stored, int64(len(f.stored)), nil
}

func newTestService() (*Service, *fakeRepo) {
	repo := &fakeRepo{}
	s := NewService(repo, config.ClientAppConfig{MinVersion: "1.2.0", LatestVersion: "1.4.0", UpdateURLAndroid: "https://play.example/greenroot"})
	s.now = func() time.Time { return time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC) }
	return s, repo
}

func TestVersionPolicy(t *testing.T) {
	s, _ := newTestService()
	cases := []struct {
		platform, version   string
		required, available bool
	}{
		{"android", "1.1.9", true, true},
		{"android", "1.2.0", false, true},
		{"ANDROID", "1.3.5+44", false, true},
		{"ios", "1.4.0", false, false},
		{"web", "2.0.0", false, false},
		{"android", "", false, false},
	}
	for _, c := range cases {
		p, err := s.VersionPolicy(c.platform, c.version)
		if err != nil {
			t.Fatalf("%s %s: %v", c.platform, c.version, err)
		}
		if p.UpdateRequired != c.required || p.UpdateAvailable != c.available {
			t.Errorf("%s %s: required=%v available=%v, want %v %v", c.platform, c.version, p.UpdateRequired, p.UpdateAvailable, c.required, c.available)
		}
	}
	if p, _ := s.VersionPolicy("android", ""); p.UpdateURL != "https://play.example/greenroot" || p.MinimumVersion != "1.2.0" {
		t.Errorf("policy fields: %+v", p)
	}
	for _, bad := range [][2]string{{"windows", "1.0.0"}, {"android", "one.two"}} {
		if _, err := s.VersionPolicy(bad[0], bad[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%v: want ErrInvalidInput, got %v", bad, err)
		}
	}
}

func TestSubmitCrashReport_ScrubsAndCaps(t *testing.T) {
	s, repo := newTestService()
	uid := int64(7)
	ctx := map[string]string{"screen": "order_detail", "mobile": "9876543210"}
	for i := 0; i < 30; i++ {
		ctx[strings.Repeat("k", i+1)] = "v"
	}
	future := s.now().Add(48 * time.Hour)
	_, err := s.SubmitCrashReport(context.Background(), &uid, "req-1", CrashReportRequest{
		Platform: "Android", AppVersion: "1.3.0", ErrorType: "StateError",
		Message:    "Bad state for ravi@example.com with token=abc123",
		StackTrace: strings.Repeat("x", maxStackLen+500),
		Context:    ctx, IsFatal: true, OccurredAt: &future,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := repo.stored[0]
	if got.Platform != "android" || *got.UserID != 7 || got.RequestID != "req-1" || !got.IsFatal {
		t.Fatalf("stored: %+v", got)
	}
	if strings.Contains(got.Message, "ravi@") || strings.Contains(got.Message, "abc123") {
		t.Errorf("message not scrubbed: %q", got.Message)
	}
	if len(got.StackTrace) != maxStackLen {
		t.Errorf("stack length %d, want %d", len(got.StackTrace), maxStackLen)
	}
	if len(got.Context) != maxContextKeys {
		t.Errorf("context keys %d, want %d", len(got.Context), maxContextKeys)
	}
	for _, v := range got.Context {
		if strings.Contains(v, "9876543210") {
			t.Error("context value not scrubbed")
		}
	}
	if !got.OccurredAt.Equal(s.now()) {
		t.Errorf("a far-future device time must fall back to receive time, got %v", got.OccurredAt)
	}
}

func TestSubmitCrashReport_RequiresFields(t *testing.T) {
	s, repo := newTestService()
	for _, in := range []CrashReportRequest{
		{Platform: "symbian", AppVersion: "1", ErrorType: "E", Message: "m"},
		{Platform: "ios", ErrorType: "E", Message: "m"},
		{Platform: "ios", AppVersion: "1", Message: "m"},
		{Platform: "ios", AppVersion: "1", ErrorType: "E", Message: "  "},
	} {
		if _, err := s.SubmitCrashReport(context.Background(), nil, "", in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: want ErrInvalidInput, got %v", in, err)
		}
	}
	if len(repo.stored) != 0 {
		t.Fatal("invalid reports must not be stored")
	}
}

func TestListCrashReports_AdminOnlyAndPaged(t *testing.T) {
	s, repo := newTestService()
	if _, _, err := s.ListCrashReports(context.Background(), false, ListCrashReportsRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin: want ErrForbidden, got %v", err)
	}
	if _, _, err := s.ListCrashReports(context.Background(), true, ListCrashReportsRequest{PerPage: 500}); err != nil {
		t.Fatal(err)
	}
	if repo.listed.Page != 1 || repo.listed.PerPage != 25 {
		t.Errorf("paging defaults: %+v", repo.listed)
	}
	if _, _, err := s.ListCrashReports(context.Background(), true, ListCrashReportsRequest{Platform: "beos"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad platform filter: %v", err)
	}
}
