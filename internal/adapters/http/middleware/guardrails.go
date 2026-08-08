package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type ActorLimiter interface{ Allow(uuid.UUID) bool }
type ActorRateLimiter struct {
	mu          sync.Mutex
	items       map[uuid.UUID]actorBucket
	rate, burst float64
	maximum     int
	idleExpiry  time.Duration
	now         func() time.Time
}
type actorBucket struct {
	tokens     float64
	last, seen time.Time
}

func NewActorRateLimiter(rate, burst, maximum int, idleExpiry time.Duration) (*ActorRateLimiter, error) {
	if rate < 1 || burst < 1 || maximum < 1 || idleExpiry < time.Second {
		return nil, fmt.Errorf("actor rate limiter values must be positive")
	}
	return &ActorRateLimiter{items: make(map[uuid.UUID]actorBucket), rate: float64(rate), burst: float64(burst), maximum: maximum, idleExpiry: idleExpiry, now: time.Now}, nil
}
func (l *ActorRateLimiter) Allow(actorID uuid.UUID) bool {
	if l == nil || actorID == uuid.Nil {
		return false
	}
	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, exists := l.items[actorID]
	if !exists {
		if len(l.items) >= l.maximum {
			threshold := now.Add(-l.idleExpiry)
			for id, item := range l.items {
				if !item.seen.After(threshold) {
					delete(l.items, id)
				}
			}
		}
		if len(l.items) >= l.maximum {
			return false
		}
		bucket = actorBucket{tokens: l.burst, last: now, seen: now}
	}
	if now.Before(bucket.last) {
		now = bucket.last
	}
	bucket.tokens = min(l.burst, bucket.tokens+now.Sub(bucket.last).Seconds()*l.rate)
	bucket.last = now
	bucket.seen = now
	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	l.items[actorID] = bucket
	return allowed
}
func RateLimitActor(limiter ActorLimiter, writeError ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := Actor(r)
			if actor.UserID == uuid.Nil {
				writeError(w, r, domain.ErrAuthentication)
				return
			}
			if limiter == nil || !limiter.Allow(actor.UserID) {
				w.Header().Set("Retry-After", "1")
				writeError(w, r, domain.ErrRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func AssignRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), requestID)))
	})
}

func RecoverPanics(writeError ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("dialog panic request_id=%s: %v", RequestID(r), recovered)
					writeError(w, r, errInternal)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type internalError struct{}

func (internalError) Error() string { return "internal error" }

var errInternal error = internalError{}
