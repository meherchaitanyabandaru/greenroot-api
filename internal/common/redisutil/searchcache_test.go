package redisutil

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	return client
}

type searchPayload struct {
	Names []string `json:"names"`
}

func TestSearchCacheMissThenHit(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	gen := SearchGeneration(ctx, client, nil, "plants", 0)
	if gen != 0 {
		t.Fatalf("expected generation 0 before any bump, got %d", gen)
	}
	key := SearchCacheKey("plants", 0, gen, "rose")

	if _, ok := GetCachedSearch[searchPayload](ctx, client, nil, "plants", "rose", key); ok {
		t.Fatal("expected a miss before anything is stored")
	}

	want := searchPayload{Names: []string{"Rose", "Rosa"}}
	SetCachedSearch(ctx, client, nil, "plants", "rose", key, want, SearchTTLPlants, 0)

	got, ok := GetCachedSearch[searchPayload](ctx, client, nil, "plants", "rose", key)
	if !ok {
		t.Fatal("expected a hit after SetCachedSearch")
	}
	if len(got.Names) != 2 || got.Names[0] != "Rose" {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBumpSearchGenerationInvalidatesOldKey(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	gen := SearchGeneration(ctx, client, nil, "orders", 7)
	staleKey := SearchCacheKey("orders", 7, gen, "GR-ORD-1")
	SetCachedSearch(ctx, client, nil, "orders", "GR-ORD-1", staleKey, searchPayload{Names: []string{"stale"}}, SearchTTLOrders, 0)

	if _, ok := GetCachedSearch[searchPayload](ctx, client, nil, "orders", "GR-ORD-1", staleKey); !ok {
		t.Fatal("expected the pre-bump entry to be readable")
	}

	// A mutation (order created/updated/cancelled) bumps this user's
	// generation -- every subsequently-computed key changes, so the old
	// entry becomes unreachable even though it hasn't expired yet.
	BumpSearchGeneration(ctx, client, nil, "orders", 7)

	newGen := SearchGeneration(ctx, client, nil, "orders", 7)
	if newGen != gen+1 {
		t.Fatalf("expected generation to increment to %d, got %d", gen+1, newGen)
	}
	newKey := SearchCacheKey("orders", 7, newGen, "GR-ORD-1")
	if newKey == staleKey {
		t.Fatal("expected the cache key to change after a generation bump")
	}
	if _, ok := GetCachedSearch[searchPayload](ctx, client, nil, "orders", "GR-ORD-1", newKey); ok {
		t.Fatal("expected a miss on the new key -- it was never populated")
	}
}

func TestSearchCacheKeyIsScopedPerUser(t *testing.T) {
	gen := int64(0)
	keyUserA := SearchCacheKey("orders", 1, gen, "rose")
	keyUserB := SearchCacheKey("orders", 2, gen, "rose")
	if keyUserA == keyUserB {
		t.Fatal("two different users must never resolve to the same cache key for an identical query")
	}
}

func TestRecentSearchesRecordDedupeAndTrim(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	RecordRecentSearch(ctx, client, nil, 1, "rose")
	RecordRecentSearch(ctx, client, nil, 1, "mango")
	RecordRecentSearch(ctx, client, nil, 1, "rose") // re-search -> moves to front, no duplicate

	got := RecentSearches(ctx, client, nil, 1)
	want := []string{"rose", "mango"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	for i := 0; i < 15; i++ {
		RecordRecentSearch(ctx, client, nil, 1, "term"+string(rune('a'+i)))
	}
	got = RecentSearches(ctx, client, nil, 1)
	if len(got) != maxRecentSearches {
		t.Fatalf("expected trimming to %d entries, got %d", maxRecentSearches, len(got))
	}
}

func TestRecentSearchesNeverSharedAcrossUsers(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	RecordRecentSearch(ctx, client, nil, 1, "rose")
	RecordRecentSearch(ctx, client, nil, 2, "mango")

	if got := RecentSearches(ctx, client, nil, 1); len(got) != 1 || got[0] != "rose" {
		t.Fatalf("user 1 history leaked or missing: %v", got)
	}
	if got := RecentSearches(ctx, client, nil, 2); len(got) != 1 || got[0] != "mango" {
		t.Fatalf("user 2 history leaked or missing: %v", got)
	}
}

func TestClearRecentSearches(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	RecordRecentSearch(ctx, client, nil, 1, "rose")
	ClearRecentSearches(ctx, client, nil, 1)

	if got := RecentSearches(ctx, client, nil, 1); len(got) != 0 {
		t.Fatalf("expected empty history after clear, got %v", got)
	}
}

func TestSearchSuggestionsRankedByPopularity(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	RecordSearchTerm(ctx, client, nil, "plants", "rose")
	RecordSearchTerm(ctx, client, nil, "plants", "rose")
	RecordSearchTerm(ctx, client, nil, "plants", "rose")
	RecordSearchTerm(ctx, client, nil, "plants", "mango")
	RecordSearchTerm(ctx, client, nil, "plants", "Rose") // case-insensitive: should merge with "rose"

	got := SearchSuggestions(ctx, client, nil, "plants", 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct terms, got %v", got)
	}
	if got[0] != "rose" {
		t.Fatalf("expected 'rose' (4 hits) to rank above 'mango' (1 hit), got %v", got)
	}
}

func TestCacheHelpersAreNilClientSafe(t *testing.T) {
	ctx := context.Background()

	// Every function in this file must degrade to a harmless no-op /
	// cache-miss when Redis is unavailable -- caching must never be able
	// to make a request fail.
	if gen := SearchGeneration(ctx, nil, nil, "plants", 0); gen != 0 {
		t.Fatalf("expected 0, got %d", gen)
	}
	BumpSearchGeneration(ctx, nil, nil, "plants", 0)
	if _, ok := GetCachedSearch[searchPayload](ctx, nil, nil, "plants", "rose", "any-key"); ok {
		t.Fatal("expected a miss with a nil client")
	}
	SetCachedSearch(ctx, nil, nil, "plants", "rose", "any-key", searchPayload{}, SearchTTLPlants, 0)
	RecordRecentSearch(ctx, nil, nil, 1, "rose")
	if got := RecentSearches(ctx, nil, nil, 1); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	ClearRecentSearches(ctx, nil, nil, 1)
	RecordSearchTerm(ctx, nil, nil, "plants", "rose")
	if got := SearchSuggestions(ctx, nil, nil, "plants", 5); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
