package clientapp

import (
	"strings"
	"testing"
)

func TestScrubRemovesSecretsAndPersonalData(t *testing.T) {
	cases := map[string]string{
		"auth failed for eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U": "[jwt]",
		"Authorization: Bearer abc.def-123":     "Bearer [redacted]",
		"otp=123456 rejected":                   "otp=[redacted] rejected",
		`{"password":"hunter2"}`:                `password":"[redacted]`,
		"user ravi.buyer@example.com not found": "[email]",
		"call 9876543210 failed":                "[mobile]",
		"call +91 9876543210 failed":            "[mobile]",
		"token: s3cr3t":                         "token: [redacted]",
	}
	for in, want := range cases {
		got := scrub(in, maxMessageLen)
		if !strings.Contains(got, want) {
			t.Errorf("scrub(%q) = %q, want it to contain %q", in, got, want)
		}
		for _, leak := range []string{"hunter2", "123456", "9876543210", "ravi.buyer", "s3cr3t", "abc.def-123", "dozjgNryP4J3"} {
			if strings.Contains(in, leak) && strings.Contains(got, leak) {
				t.Errorf("scrub(%q) leaked %q: %q", in, leak, got)
			}
		}
	}
}

func TestScrubKeepsStackTracesReadable(t *testing.T) {
	stack := "#0      OrderScreen.build (package:greenroot/features/orders/order_detail_screen.dart:1234:56)\n#1      StatelessElement.build"
	if got := scrub(stack, maxStackLen); got != stack {
		t.Fatalf("stack trace changed:\n%s", got)
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	s := strings.Repeat("₹", 10) // 3 bytes each
	got := truncate(s, 7)
	if got != "₹₹" {
		t.Fatalf("truncate = %q, want two whole runes", got)
	}
	if truncate("short", 100) != "short" {
		t.Fatal("short strings are unchanged")
	}
}
