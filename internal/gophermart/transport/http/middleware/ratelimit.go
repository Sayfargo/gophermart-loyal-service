package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
)

type RateLimiter interface {
	Incr(key string, window time.Duration) (int, time.Time)
}

func RateLimit(limiter RateLimiter, cfg ratelimitstore.RateLimitConfig, keyPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			key := fmt.Sprintf("rate:%s:%s", keyPrefix, ip)
			count, exp := limiter.Incr(key, cfg.Window)

			if count > cfg.MaxRequests {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", time.Until(exp).Seconds()))
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
