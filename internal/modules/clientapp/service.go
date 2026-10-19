package clientapp

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/semver"
)

var (
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrForbidden    = apperrs.ErrForbidden
)

// Service applies the version policy and prepares crash reports.
type Service struct {
	repo   Repository
	policy config.ClientAppConfig
	now    func() time.Time
}

// NewService returns a Service for the configured version policy.
func NewService(repo Repository, policy config.ClientAppConfig) *Service {
	return &Service{repo: repo, policy: policy, now: time.Now}
}

// VersionPolicy answers "must / may this app update?". current is optional.
func (s *Service) VersionPolicy(platform, current string) (VersionPolicy, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if !platforms[platform] {
		return VersionPolicy{}, ErrInvalidInput
	}
	p := VersionPolicy{
		Platform:       platform,
		MinimumVersion: s.policy.MinVersion,
		LatestVersion:  s.policy.LatestVersion,
	}
	switch platform {
	case "android":
		p.UpdateURL = s.policy.UpdateURLAndroid
	case "ios":
		p.UpdateURL = s.policy.UpdateURLIOS
	}
	if strings.TrimSpace(current) == "" {
		return p, nil
	}
	cur, err := semver.Parse(current)
	if err != nil {
		return VersionPolicy{}, ErrInvalidInput
	}
	p.CurrentVersion = cur.String()
	if minV, err := semver.Parse(s.policy.MinVersion); err == nil {
		p.UpdateRequired = cur.Less(minV)
	}
	if latestV, err := semver.Parse(s.policy.LatestVersion); err == nil {
		p.UpdateAvailable = cur.Less(latestV)
	}
	return p, nil
}

// SubmitCrashReport validates, scrubs and stores a report.
func (s *Service) SubmitCrashReport(ctx context.Context, userID *int64, requestID string, in CrashReportRequest) (int64, error) {
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if !platforms[platform] || strings.TrimSpace(in.AppVersion) == "" ||
		strings.TrimSpace(in.ErrorType) == "" || strings.TrimSpace(in.Message) == "" {
		return 0, ErrInvalidInput
	}
	occurred := s.now()
	// A device clock can be wrong; keep its time unless it is implausible.
	if in.OccurredAt != nil && in.OccurredAt.Before(occurred.Add(5*time.Minute)) && in.OccurredAt.After(occurred.Add(-30*24*time.Hour)) {
		occurred = *in.OccurredAt
	}
	report := CrashReport{
		UserID:      userID,
		Platform:    platform,
		AppVersion:  truncate(strings.TrimSpace(in.AppVersion), maxVersionLen),
		BuildNumber: truncate(strings.TrimSpace(in.BuildNumber), maxShortLen),
		OSVersion:   scrub(in.OSVersion, maxShortLen),
		DeviceModel: scrub(in.DeviceModel, maxShortLen),
		ErrorType:   scrub(in.ErrorType, maxErrorTypeLen),
		Message:     scrub(in.Message, maxMessageLen),
		StackTrace:  scrub(in.StackTrace, maxStackLen),
		Context:     scrubContext(in.Context),
		IsFatal:     in.IsFatal,
		OccurredAt:  occurred,
		RequestID:   requestID,
	}
	return s.repo.InsertCrashReport(ctx, report)
}

// ListCrashReports is for platform admins.
func (s *Service) ListCrashReports(ctx context.Context, isAdmin bool, in ListCrashReportsRequest) ([]CrashReport, int64, error) {
	if !isAdmin {
		return nil, 0, ErrForbidden
	}
	if in.Page < 1 {
		in.Page = 1
	}
	if in.PerPage < 1 || in.PerPage > 100 {
		in.PerPage = 25
	}
	in.Platform = strings.ToLower(strings.TrimSpace(in.Platform))
	if in.Platform != "" && !platforms[in.Platform] {
		return nil, 0, ErrInvalidInput
	}
	return s.repo.ListCrashReports(ctx, in)
}

// scrubContext keeps at most maxContextKeys keys (sorted, so the choice is
// deterministic) and scrubs every value.
func scrubContext(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(out) == maxContextKeys {
			break
		}
		key := truncate(strings.TrimSpace(k), 60)
		if key == "" {
			continue
		}
		out[key] = scrub(in[k], maxContextValue)
	}
	return out
}
