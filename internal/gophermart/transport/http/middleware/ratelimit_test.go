package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
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

func TestRateLimit_CallsNextHandler(t *testing.T) {
	t.Parallel()

	mock := &rateLimiterMock{
		count: 1,
		exp:   time.Now().Add(time.Minute),
	}

	cfg := ratelimitstore.RateLimitConfig{
		Window:      time.Minute,
		MaxRequests: 5,
	}

	called := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	rec := httptest.NewRecorder()

	RateLimit(mock, cfg, "login")(handler).ServeHTTP(rec, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "rate:login:127.0.0.1", mock.key)
	require.Equal(t, time.Minute, mock.window)
}

func TestRateLimit_TooManyRequests(t *testing.T) {
	t.Parallel()

	mock := &rateLimiterMock{
		count: 6,
		exp:   time.Now().Add(30 * time.Second),
	}

	cfg := ratelimitstore.RateLimitConfig{
		Window:      time.Minute,
		MaxRequests: 5,
	}

	called := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	rec := httptest.NewRecorder()

	RateLimit(mock, cfg, "login")(handler).ServeHTTP(rec, req)

	require.False(t, called)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestRateLimit_InvalidRemoteAddr(t *testing.T) {
	t.Parallel()

	mock := &rateLimiterMock{}

	cfg := ratelimitstore.RateLimitConfig{
		Window:      time.Minute,
		MaxRequests: 5,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "invalid"

	rec := httptest.NewRecorder()

	RateLimit(mock, cfg, "login")(handler).ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRateLimit_UsesLimiterArguments(t *testing.T) {
	t.Parallel()

	mock := &rateLimiterMock{
		count: 1,
		exp:   time.Now().Add(time.Minute),
	}

	cfg := ratelimitstore.RateLimitConfig{
		Window:      5 * time.Minute,
		MaxRequests: 10,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	req.RemoteAddr = "192.168.1.15:8080"

	rec := httptest.NewRecorder()

	RateLimit(mock, cfg, "register")(handler).ServeHTTP(rec, req)

	require.Equal(t, "rate:register:192.168.1.15", mock.key)
	require.Equal(t, 5*time.Minute, mock.window)
}
