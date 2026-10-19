// Package phone holds the one rule for Indian mobile numbers. Every place a
// mobile number enters the system (login, invites, buyers on orders,
// quotation recipients, SMS) normalises it here, so "+91 98765 43210",
// "098765 43210" and "9876543210" are the same person. The users table
// enforces the same shape (ck_users_mobile).
package phone

import (
	"regexp"
	"strings"
)

var mobile = regexp.MustCompile(`^[6-9][0-9]{9}$`)

// Normalize strips spaces, dashes, brackets and a +91 / 91 / 0 prefix and
// reports whether the result is a valid 10-digit Indian mobile number.
func Normalize(raw string) (string, bool) {
	m := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(m, "+91") && len(m) == 13:
		m = m[3:]
	case strings.HasPrefix(m, "91") && len(m) == 12:
		m = m[2:]
	case strings.HasPrefix(m, "0") && len(m) == 11:
		m = m[1:]
	}
	return m, mobile.MatchString(m)
}
