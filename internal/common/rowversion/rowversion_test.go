package rowversion

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		ok     bool
		failed bool
	}{
		{"", 0, false, false},
		{"*", 0, false, false},
		{"3", 3, true, false},
		{`"3"`, 3, true, false},
		{`W/"12"`, 12, true, false},
		{" 7 ", 7, true, false},
		{"0", 0, false, true},
		{"-1", 0, false, true},
		{`"abc"`, 0, false, true},
		{"3.5", 0, false, true},
	}
	for _, tc := range cases {
		n, ok, err := Parse(tc.in)
		if (err != nil) != tc.failed || ok != tc.ok || n != tc.want {
			t.Errorf("Parse(%q) = %d, %v, %v", tc.in, n, ok, err)
		}
		if tc.failed && !errors.Is(err, ErrMalformed) {
			t.Errorf("Parse(%q): want ErrMalformed", tc.in)
		}
	}
}

func TestMiddleware(t *testing.T) {
	var got any
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = Arg(r.Context()) }))

	r := httptest.NewRequest(http.MethodPut, "/", nil)
	r.Header.Set("If-Match", `"4"`)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != int64(4) {
		t.Fatalf("Arg = %v, want 4", got)
	}

	got = "unset"
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/", nil))
	if got != nil {
		t.Fatalf("Arg without If-Match = %v, want nil", got)
	}

	rec := httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodPut, "/", nil)
	bad.Header.Set("If-Match", "banana")
	h.ServeHTTP(rec, bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed If-Match = %d, want 400", rec.Code)
	}
}

func TestWithAndExpected(t *testing.T) {
	if _, ok := Expected(context.Background()); ok {
		t.Fatal("empty context has no expected version")
	}
	if v, ok := Expected(With(context.Background(), 9)); !ok || v != 9 {
		t.Fatalf("Expected = %d, %v", v, ok)
	}
}
