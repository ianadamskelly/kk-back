package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipRateLimiter is an in-memory per-IP token bucket. Suitable for the
// single-instance Coolify deploy. A horizontally scaled deploy would
// need a shared backend (Redis), but the limiter API stays the same.
type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*rateVisitor
	limit    rate.Limit
	burst    int
	ttl      time.Duration
}

type rateVisitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// newIPRateLimiter builds a limiter that lets each IP make `burst`
// requests in quick succession, then refills at `perMinute` events
// per minute. Idle entries are reaped after 10 minutes so the map
// doesn't grow unbounded.
func newIPRateLimiter(perMinute float64, burst int) *ipRateLimiter {
	l := &ipRateLimiter{
		visitors: make(map[string]*rateVisitor),
		limit:    rate.Limit(perMinute / 60.0), // events per second
		burst:    burst,
		ttl:      10 * time.Minute,
	}
	go l.gc()
	return l
}

func (l *ipRateLimiter) gc() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-l.ttl)
		for ip, v := range l.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(l.visitors, ip)
			}
		}
		l.mu.Unlock()
	}
}

// Allow returns true if the IP still has budget for one request.
// Always touches the visitor's lastSeen so active IPs aren't gc'd.
func (l *ipRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	v, ok := l.visitors[ip]
	if !ok {
		v = &rateVisitor{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	limiter := v.limiter
	l.mu.Unlock()
	return limiter.Allow()
}

// clientIP returns the requester's source address. Trusts the
// X-Forwarded-For / X-Real-IP headers only when the connection's
// RemoteAddr is a loopback — i.e. when the request reached us
// through a local proxy (Coolify + Traefik on the same host). Direct
// connections (dev, tests) use RemoteAddr verbatim.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopback(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First entry in the chain is the original client.
			if comma := strings.Index(xff, ","); comma > 0 {
				return strings.TrimSpace(xff[:comma])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	return host
}

func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// rateLimit is the middleware wrapper. Returns 429 with a
// Retry-After hint when the IP runs out of tokens.
func rateLimit(l *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests,
					"too many requests — please slow down and try again in a minute")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
