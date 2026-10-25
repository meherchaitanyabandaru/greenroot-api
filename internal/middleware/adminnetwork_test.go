package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/clientip"
)

func TestAdminNetwork(t *testing.T) {
	office := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	call := func(allowed []netip.Prefix, roles []string, remote string) int {
		h := clientip.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if roles != nil {
				r = r.WithContext(authctx.StoreActor(r.Context(), authctx.Actor{UserID: 1, Roles: roles}))
			}
			AdminNetwork(allowed)(ok).ServeHTTP(w, r)
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req.RemoteAddr = remote + ":1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	cases := []struct {
		name    string
		allowed []netip.Prefix
		roles   []string
		remote  string
		want    int
	}{
		{"admin from office", office, []string{"ADMIN"}, "203.0.113.7", 200},
		{"admin from elsewhere", office, []string{"ADMIN"}, "198.51.100.9", 403},
		{"super admin from elsewhere", office, []string{"SUPER_ADMIN"}, "198.51.100.9", 403},
		{"owner from anywhere", office, []string{"NURSERY_OWNER"}, "198.51.100.9", 200},
		{"anonymous from anywhere", office, nil, "198.51.100.9", 200},
		{"no list: admin anywhere", nil, []string{"ADMIN"}, "198.51.100.9", 200},
	}
	for _, c := range cases {
		if got := call(c.allowed, c.roles, c.remote); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
