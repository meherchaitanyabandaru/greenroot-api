// Package search backs the Global Search feature's Recent Searches and
// Search Suggestions -- both are inherently ephemeral, per-user or
// popularity-ranked data with no PostgreSQL counterpart (there's nothing
// to be "the source of truth" for a device's own search history), so
// unlike orders/quotations/plants/market ads, Redis isn't just a cache
// here -- it's the only store. See redisutil/searchcache.go for the
// underlying primitives and the cache-aside layer used by those other
// four modules.
package search

import (
	"context"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

var validModules = map[string]bool{
	"orders":     true,
	"quotations": true,
	"plants":     true,
	"market_ads": true,
}

type Service struct {
	redis redis.Cmdable
}

func NewService(redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{redis: rdb}
}

// RecordSearch appends query to the user's recent-search history (max 10,
// de-duplicated, most recent first, 30-day TTL).
func (s *Service) RecordSearch(ctx context.Context, userID int64, query string) {
	redisutil.RecordRecentSearch(ctx, s.redis, nil, userID, query)
}

// RecentSearches returns the user's search history, most recent first.
func (s *Service) RecentSearches(ctx context.Context, userID int64) []string {
	items := redisutil.RecentSearches(ctx, s.redis, nil, userID)
	if items == nil {
		return []string{}
	}
	return items
}

// ClearRecentSearches wipes a user's search history. Different users never
// share history (keyed by userID), and this is called on logout so the
// next person to use a shared/reset device starts clean.
func (s *Service) ClearRecentSearches(ctx context.Context, userID int64) {
	redisutil.ClearRecentSearches(ctx, s.redis, nil, userID)
}

// Suggestions returns the top popular search terms for module (defaults to
// "plants" -- the primary autocomplete-while-typing use case). Terms are
// real, derived from actual search traffic (see orders/quotations/plants/
// market services' List/BrowseAds cache-aside paths, which call
// redisutil.RecordSearchTerm), not a hardcoded list.
func (s *Service) Suggestions(ctx context.Context, module string, limit int64) []string {
	if !validModules[module] {
		module = "plants"
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	items := redisutil.SearchSuggestions(ctx, s.redis, nil, module, limit)
	if items == nil {
		return []string{}
	}
	return items
}
