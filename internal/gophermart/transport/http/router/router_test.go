package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/config"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/core/deps"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func buildDeps(t *testing.T, routes map[string]ratelimitstore.RateLimitConfig) *deps.Dependencies {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	rls := ratelimitstore.New(ctx, &ratelimitstore.Config{CleanupInterval: time.Minute}, testLogger())
	tokenSvc := authentication.New([]byte("test-secret"))

	cfg := &config.Config{
		RLS: &ratelimitstore.Config{
			CleanupInterval: time.Minute,
			Routes:          routes,
		},
	}

	return &deps.Dependencies{
		TokenSvc: tokenSvc,
		RLS:      rls,
		Cfg:      cfg,
		Logger:   testLogger(),
	}
}

func defaultRateLimitRoutes() map[string]ratelimitstore.RateLimitConfig {
	return map[string]ratelimitstore.RateLimitConfig{
		"register": {Window: time.Minute, MaxRequests: 5},
		"login":    {Window: time.Minute, MaxRequests: 10},
	}
}

type route struct {
	method string
	path   string
}

func collectRoutes(t *testing.T, r chi.Router) map[route]struct{} {
	t.Helper()

	got := make(map[route]struct{})
	err := chi.Walk(r, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[route{method: method, path: strings.TrimSuffix(path, "/")}] = struct{}{}
		return nil
	})
	require.NoError(t, err)
	return got
}

func TestSetupRoutes_RegistersExpectedRouteTable(t *testing.T) {
	r := chi.NewRouter()
	d := buildDeps(t, defaultRateLimitRoutes())

	SetupRoutes(r, d)

	got := collectRoutes(t, r)

	want := []route{
		{http.MethodPost, "/api/user/register"},
		{http.MethodPost, "/api/user/login"},
		{http.MethodPost, "/api/user/orders"},
		{http.MethodGet, "/api/user/orders"},
		{http.MethodGet, "/api/user/balance"},
		{http.MethodPost, "/api/user/balance/withdraw"},
		{http.MethodGet, "/api/user/withdrawals"},
	}

	for _, rt := range want {
		assert.Containsf(t, got, rt, "ожидался маршрут %s %s", rt.method, rt.path)
	}
	assert.Len(t, got, len(want), "обнаружены лишние или отсутствующие маршруты: %+v", got)
}

func TestRegisterUserRoutes_PanicsWithoutRegisterConfig(t *testing.T) {
	routes := defaultRateLimitRoutes()
	delete(routes, "register")
	d := buildDeps(t, routes)

	assert.PanicsWithValue(t, "register rate limit config not found", func() {
		registerUserRoutes(chi.NewRouter(), d)
	})
}

func TestRegisterUserRoutes_PanicsWithoutLoginConfig(t *testing.T) {
	routes := defaultRateLimitRoutes()
	delete(routes, "login")
	d := buildDeps(t, routes)

	assert.PanicsWithValue(t, "login rate limit config not found", func() {
		registerUserRoutes(chi.NewRouter(), d)
	})
}

func TestRegisterUserRoutes_OKWithBothConfigsPresent(t *testing.T) {
	d := buildDeps(t, defaultRateLimitRoutes())

	assert.NotPanics(t, func() {
		registerUserRoutes(chi.NewRouter(), d)
	})
}
