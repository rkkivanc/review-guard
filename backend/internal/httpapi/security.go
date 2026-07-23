package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a simple per-key fixed-window limiter (in-process).
type RateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	buckets map[string]*rateBucket
}

type rateBucket struct {
	count int
	start time.Time
}

// NewRateLimiter allows `limit` events per `window` per key.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:  window,
		limit:   limit,
		buckets: make(map[string]*rateBucket),
	}
}

func (rl *RateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.start) >= rl.window {
		rl.buckets[key] = &rateBucket{count: 1, start: now}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// Middleware rate-limits by client IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			WriteErr(w, http.StatusTooManyRequests, "rate_limited", "too many requests — slow down and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// SecurityHeaders adds defense-in-depth response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// RedactSensitiveQuery removes secrets from the request URL before logging middleware runs.
func RedactSensitiveQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL != nil && r.URL.RawQuery != "" {
			q := r.URL.Query()
			changed := false
			for _, key := range []string{"refresh_token", "token", "access_token", "password"} {
				if q.Has(key) {
					q.Set(key, "[redacted]")
					changed = true
				}
			}
			if changed {
				r.URL.RawQuery = q.Encode()
			}
		}
		next.ServeHTTP(w, r)
	})
}
