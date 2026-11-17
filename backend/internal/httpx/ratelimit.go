package httpx

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter applies a token bucket per client IP to protect sensitive endpoints.
type IPRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    rate.Limit
	burst    int
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter builds a limiter allowing `events` per interval with the given burst.
func NewIPRateLimiter(every time.Duration, burst int) *IPRateLimiter {
	l := &IPRateLimiter{
		visitors: make(map[string]*visitor),
		limit:    rate.Every(every),
		burst:    burst,
	}
	go l.cleanup()
	return l
}

// Allow reports whether the request from this remote address is permitted.
func (l *IPRateLimiter) Allow(r *http.Request) bool {
	// ClientIP matches the audit/logging resolver: proxy headers count only
	// from local-network peers, so attackers cannot rotate limits by forging
	// X-Forwarded-For when the API is exposed directly.
	ip := ClientIP(r)

	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

func (l *IPRateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		l.mu.Lock()
		for ip, v := range l.visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(l.visitors, ip)
			}
		}
		l.mu.Unlock()
	}
}
