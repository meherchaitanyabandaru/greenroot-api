// Package revocation provides an in-process, zero-dependency revocation store.
// When an admin suspends a user, their user_id is added here so the next request
// is rejected within milliseconds — no Redis required.
//
// Entries expire after the access token TTL (default 15 min), so the map stays tiny.
// On multi-instance deployments, replace Revoke() with a pub/sub broadcast; the
// per-instance map stays and just acts as a local cache of the broadcast.
package revocation

import (
	"sync"
	"time"
)

var global = &store{entries: make(map[int64]time.Time)}

type store struct {
	mu      sync.RWMutex
	entries map[int64]time.Time // userID → revocation expires_at
}

// Revoke marks userID as revoked for the given duration.
// Calling Revoke again resets (extends) the window.
func Revoke(userID int64, ttl time.Duration) {
	global.mu.Lock()
	global.entries[userID] = time.Now().Add(ttl)
	global.mu.Unlock()
}

// IsRevoked reports whether userID is currently in the revocation window.
func IsRevoked(userID int64) bool {
	global.mu.RLock()
	exp, ok := global.entries[userID]
	global.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

// Remove clears a revocation entry — call when admin reinstates a user.
func Remove(userID int64) {
	global.mu.Lock()
	delete(global.entries, userID)
	global.mu.Unlock()
}

// Cleanup removes expired entries. Call periodically (e.g. from the cron goroutine).
func Cleanup() {
	now := time.Now()
	global.mu.Lock()
	for id, exp := range global.entries {
		if now.After(exp) {
			delete(global.entries, id)
		}
	}
	global.mu.Unlock()
}
