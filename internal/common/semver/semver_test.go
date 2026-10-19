package semver

import "testing"

func TestParseAndLess(t *testing.T) {
	cases := []struct {
		in   string
		want Version
	}{
		{"1.4.2", Version{1, 4, 2}}, {"1.4", Version{1, 4, 0}}, {"2", Version{2, 0, 0}},
		{"v1.4.2", Version{1, 4, 2}}, {"1.4.2+37", Version{1, 4, 2}}, {"1.4.2-beta", Version{1, 4, 2}},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %v, %v", c.in, got, err)
		}
	}
	for _, bad := range []string{"", "a.b", "1.2.3.4", "1.-1", "1..2"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q): want error", bad)
		}
	}
	ordered := []string{"0.9.9", "1.0.0", "1.0.1", "1.2.0", "1.10.0", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		a, _ := Parse(ordered[i-1])
		b, _ := Parse(ordered[i])
		if !a.Less(b) || b.Less(a) {
			t.Errorf("%s should be older than %s", a, b)
		}
	}
	x, _ := Parse("1.2.3")
	if x.Less(x) {
		t.Error("a version is not older than itself")
	}
}
