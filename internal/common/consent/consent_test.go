package consent

import (
	"errors"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	cur := Templates[DriverDispatch].Version
	cases := []struct {
		name string
		g    *Given
		want error
	}{
		{"not given", nil, ErrRequired},
		{"shown but not accepted", &Given{Version: cur}, ErrRequired},
		{"older wording", &Given{Version: cur - 1, Accepted: true}, ErrOutdated},
		{"current wording accepted", &Given{Version: cur, Accepted: true}, nil},
	}
	for _, c := range cases {
		if err := Check(DriverDispatch, c.g); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestTemplatesNeverWaiveRights(t *testing.T) {
	for step, tpl := range Templates {
		if tpl.Type != step || tpl.Version < 1 || len(tpl.Statements) == 0 {
			t.Errorf("%s: incomplete template %+v", step, tpl)
		}
	}
	// The buyer is told their consumer rights are not affected, and that a
	// hidden problem can still be reported after confirming delivery.
	if s := Templates[BuyerQuotationAccept].Statements; !strings.Contains(s[len(s)-1], "rights under consumer law are not affected") {
		t.Error("buyer acceptance must say rights are not affected")
	}
	if s := Templates[BuyerDelivery].Statements; !strings.Contains(s[len(s)-1], "can still be reported") {
		t.Error("confirming delivery must not stop a later complaint")
	}
}
