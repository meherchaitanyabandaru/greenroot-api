package clientapp

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Personal data and credentials must not be stored in crash reports
// (standard D12). Patterns are applied to every free-text field.
var scrubbers = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[jwt]"},
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer [redacted]"},
	// key=value, key: value, "key":"value" and "key value".
	{regexp.MustCompile(`(?i)\b(otp|password|passwd|secret|token|api[_-]?key)\b("?\s*[:=]\s*"?|\s+)[^\s",}]+`), "$1$2[redacted]"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "[email]"},
	// Indian mobile numbers, with or without +91/0 prefix.
	{regexp.MustCompile(`(?:\+91[\s-]?|\b0)?\b[6-9]\d{9}\b`), "[mobile]"},
}

// scrub removes credentials and personal data, then caps the length.
func scrub(s string, max int) string {
	for _, sc := range scrubbers {
		s = sc.re.ReplaceAllString(s, sc.with)
	}
	return truncate(strings.TrimSpace(s), max)
}

// truncate cuts s to at most max bytes without splitting a UTF-8 rune.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
