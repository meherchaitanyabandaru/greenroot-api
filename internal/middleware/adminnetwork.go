package middleware

import (
	"net/http"
	"net/netip"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/clientip"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// AdminNetwork limits ADMIN / SUPER_ADMIN accounts to the listed networks
// (ADMIN_ALLOWED_CIDRS, e.g. the office or a VPN): a stolen admin token or
// SIM-swapped OTP is useless from anywhere else. Empty = no restriction.
// Must run after EnrichActorMiddleware.
func AdminNetwork(allowed []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := authctx.ActorFromContext(r.Context())
			if ok && actor.HasRole("ADMIN", "SUPER_ADMIN") && !inPrefixes(clientip.From(r), allowed) {
				response.Error(w, http.StatusForbidden, "admin_network_not_allowed",
					"admin accounts can only be used from the company network")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func inPrefixes(ip string, prefixes []netip.Prefix) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
