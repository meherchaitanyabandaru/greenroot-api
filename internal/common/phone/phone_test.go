package phone

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"9876543210":      "9876543210",
		"+91 98765 43210": "9876543210",
		"+919876543210":   "9876543210",
		"919876543210":    "9876543210",
		"09876543210":     "9876543210",
		"(98765) 43-210":  "9876543210",
		" 6123456789 ":    "6123456789",
	} {
		if got, ok := Normalize(in); !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "12345", "0000000000", "1234567890", "5876543210", "98765432101", "abcdefghij", "+1 9876543210"} {
		if _, ok := Normalize(bad); ok {
			t.Errorf("Normalize(%q) accepted", bad)
		}
	}
}
