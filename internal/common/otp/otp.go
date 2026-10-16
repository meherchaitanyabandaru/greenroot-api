// Package otp issues and verifies one-time codes in Redis for any channel
// (login SMS, email verification). Codes are stored hashed, are single-use,
// burn after too many wrong guesses, and sends are throttled per identity.
package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrCooldown    = errors.New("otp requested too soon")
	ErrRateLimited = errors.New("too many otp requests")
)

// Policy is per use-case; Prefix keeps the key spaces apart.
type Policy struct {
	Prefix      string // e.g. "otp:" or "email_otp:"
	TTL         time.Duration
	Cooldown    time.Duration
	MaxPerHour  int64
	MaxAttempts int64
}

type Store struct {
	rdb redis.Cmdable
	p   Policy
}

// New accepts a nil client: Save becomes a no-op and Verify falls back to the
// dev code (mock mode only — live callers must require Redis themselves).
func New(rdb redis.Cmdable, p Policy) *Store { return &Store{rdb: rdb, p: p} }

func (s *Store) Enabled() bool { return s.rdb != nil }

func (s *Store) key(id string) string      { return s.p.Prefix + id }
func (s *Store) attempts(id string) string { return s.p.Prefix + "attempts:" + id }

// Generate returns 6 random digits from crypto/rand.
func Generate() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

func hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// Throttle allows one send per Cooldown and MaxPerHour sends per identity.
func (s *Store) Throttle(ctx context.Context, id string) error {
	ok, err := s.rdb.SetNX(ctx, s.p.Prefix+"cooldown:"+id, 1, s.p.Cooldown).Result()
	if err != nil {
		return fmt.Errorf("otp throttle: %w", err)
	}
	if !ok {
		return ErrCooldown
	}
	hourKey := s.p.Prefix + "hourly:" + id
	count, err := s.rdb.Incr(ctx, hourKey).Result()
	if err != nil {
		return fmt.Errorf("otp throttle: %w", err)
	}
	if count == 1 {
		s.rdb.Expire(ctx, hourKey, time.Hour)
	}
	if count > s.p.MaxPerHour {
		return ErrRateLimited
	}
	return nil
}

// Save stores the hashed code and resets the wrong-guess counter.
func (s *Store) Save(ctx context.Context, id, code string) error {
	if s.rdb == nil {
		return nil
	}
	if err := s.rdb.Set(ctx, s.key(id), hash(code), s.p.TTL).Err(); err != nil {
		return fmt.Errorf("store otp: %w", err)
	}
	s.rdb.Del(ctx, s.attempts(id))
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, s.key(id), s.attempts(id))
	}
}

// Verify checks and burns a code. devCode, when non-empty, is accepted if no
// code is stored (mock mode only); pass "" for live channels.
func (s *Store) Verify(ctx context.Context, id, code, devCode string) bool {
	devFallback := func() bool { return devCode != "" && code == devCode }
	if s.rdb == nil {
		return devFallback()
	}
	stored, err := s.rdb.Get(ctx, s.key(id)).Result()
	if errors.Is(err, redis.Nil) {
		return devFallback()
	}
	if err != nil {
		slog.Warn("redis otp get failed", "key", s.key(id), "error", err)
		return devFallback()
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(hash(code))) != 1 {
		// Burn after too many wrong guesses so the code cannot be brute-forced
		// from many IPs within its TTL.
		n, _ := s.rdb.Incr(ctx, s.attempts(id)).Result()
		if n == 1 {
			s.rdb.Expire(ctx, s.attempts(id), s.p.TTL)
		}
		if n >= s.p.MaxAttempts {
			s.Delete(ctx, id)
		}
		return false
	}
	s.Delete(ctx, id)
	return true
}
