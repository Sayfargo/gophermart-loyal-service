package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rateLimiterMock struct {
	count int
	exp   time.Time

	key    string
	window time.Duration
}

func (m *rateLimiterMock) Incr(key string, window time.Duration) (int, time.Time) {
	m.key = key
	m.window = window

	return m.count, m.exp
}

func TestGlobalRateLimit_AllowsRequest(t *testing.T) {
	t.Parallel()

	cfg := ratelimitstore.RateLimitConfig{
		MaxRequests: 10,
		Window:      time.Minute,
	}

	limiter := &rateLimiterMock{
		count: 1,
		exp:   time.Now().Add(time.Minute),
	}

	called := false

	handler := GlobalRateLimit(limiter, cfg, "accrual")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, "rate:accrual:global", limiter.key)
	assert.Equal(t, cfg.Window, limiter.window)
}

func TestGlobalRateLimit_TooManyRequests(t *testing.T) {
	t.Parallel()

	cfg := ratelimitstore.RateLimitConfig{
		MaxRequests: 10,
		Window:      time.Minute,
	}

	limiter := &rateLimiterMock{
		count: 11,
		exp:   time.Now().Add(30 * time.Second),
	}

	called := false

	handler := GlobalRateLimit(limiter, cfg, "")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	retryAfter := rec.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter)
}
