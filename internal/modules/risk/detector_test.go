package risk

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every rule seed-master.sql seeds has a check, and every check a rule. The
// seeds live in greenroot-infra: GREENROOT_SEED_SQL (CI integration job) or
// the sibling checkout; skipped when neither is there.
func TestEverySeededRuleHasACheck(t *testing.T) {
	path := os.Getenv("GREENROOT_SEED_SQL")
	if path == "" {
		path = "../../../../greenroot-infra/db/postgresql/seed-master.sql"
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skipf("seed file not found at %s", path)
	}
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
