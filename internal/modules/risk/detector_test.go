package risk

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every rule the migration seeds has a check, and every check a rule.
func TestEverySeededRuleHasACheck(t *testing.T) {
	raw, err := os.ReadFile("../../database/migrations/20261030_05_consent_and_risk.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for code := range checks {
		if !strings.Contains(string(raw), "('"+code+"'") {
			t.Errorf("check %s has no seeded rule", code)
		}
	}
	seeded := regexp.MustCompile(`\('([A-Z_]+)', \d+, '(LOW|MEDIUM|HIGH)'`).FindAllStringSubmatch(string(raw), -1)
	for _, m := range seeded {
		if _, ok := checks[m[1]]; !ok {
			t.Errorf("seeded rule %s has no check", m[1])
		}
	}
	if len(seeded) != len(checks) {
		t.Errorf("seeded %d rules, have %d checks", len(seeded), len(checks))
	}
}

func TestOnlyAnAdminConfirmsFraud(t *testing.T) {
	for code, q := range checks {
		if strings.Contains(q, "CONFIRMED_FRAUD") {
			t.Errorf("rule %s must not produce CONFIRMED_FRAUD", code)
		}
	}
	if !actions["CONFIRM_FRAUD"] {
		t.Error("admins need the CONFIRM_FRAUD action")
	}
}
