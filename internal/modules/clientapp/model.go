// Package clientapp serves the mobile/web app's version policy and receives
// its crash reports.
package clientapp

import "time"

// Field limits for a crash report. Longer values are truncated, not rejected,
// so a huge stack trace still produces a useful report.
const (
	maxVersionLen   = 32
	maxShortLen     = 120
	maxErrorTypeLen = 200
	maxMessageLen   = 2000
	maxStackLen     = 16000
	maxContextKeys  = 20
	maxContextValue = 200
)

var platforms = map[string]bool{"android": true, "ios": true, "web": true}

// VersionPolicy tells an app whether it must or may update.
type VersionPolicy struct {
	Platform       string `json:"platform"`
	MinimumVersion string `json:"minimum_version"`
	LatestVersion  string `json:"latest_version"`
	UpdateURL      string `json:"update_url,omitempty"`
	// Set only when the request names its version.
	CurrentVersion  string `json:"current_version,omitempty"`
	UpdateRequired  bool   `json:"update_required"`
	UpdateAvailable bool   `json:"update_available"`
}

// CrashReportRequest is the body of POST /app/crash-reports.
type CrashReportRequest struct {
	Platform    string            `json:"platform"`
	AppVersion  string            `json:"app_version"`
	BuildNumber string            `json:"build_number"`
	OSVersion   string            `json:"os_version"`
	DeviceModel string            `json:"device_model"`
	ErrorType   string            `json:"error_type"`
	Message     string            `json:"message"`
	StackTrace  string            `json:"stack_trace"`
	Context     map[string]string `json:"context"`
	IsFatal     bool              `json:"is_fatal"`
	OccurredAt  *time.Time        `json:"occurred_at"`
}

// CrashReport is a stored, scrubbed report.
type CrashReport struct {
	ID          int64             `json:"id"`
	UserID      *int64            `json:"user_id,omitempty"`
	Platform    string            `json:"platform"`
	AppVersion  string            `json:"app_version"`
	BuildNumber string            `json:"build_number,omitempty"`
	OSVersion   string            `json:"os_version,omitempty"`
	DeviceModel string            `json:"device_model,omitempty"`
	ErrorType   string            `json:"error_type"`
	Message     string            `json:"message"`
	StackTrace  string            `json:"stack_trace,omitempty"`
	Context     map[string]string `json:"context"`
	IsFatal     bool              `json:"is_fatal"`
	OccurredAt  time.Time         `json:"occurred_at"`
	ReceivedAt  time.Time         `json:"received_at"`
	RequestID   string            `json:"request_id,omitempty"`
}

// ListCrashReportsRequest filters the admin listing.
type ListCrashReportsRequest struct {
	AppVersion string
	Platform   string
	FatalOnly  bool
	Page       int
	PerPage    int
}
