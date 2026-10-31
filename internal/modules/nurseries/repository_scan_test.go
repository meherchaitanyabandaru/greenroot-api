package nurseries

import (
	"strings"
	"testing"
)

func TestBuildNurseryWhere_ApprovedIncludesReactivated(t *testing.T) {
	where, args := buildNurseryWhere(ListNurseriesRequest{NurseryStatus: "APPROVED"})
	if !strings.Contains(where, "IN ('APPROVED', 'ACTIVE')") || len(args) != 0 {
		t.Fatalf("where=%q args=%v", where, args)
	}
	where, args = buildNurseryWhere(ListNurseriesRequest{NurseryStatus: "PENDING"})
	if !strings.Contains(where, "n.status::text = $1") || len(args) != 1 || args[0] != "PENDING" {
		t.Fatalf("where=%q args=%v", where, args)
	}
}
