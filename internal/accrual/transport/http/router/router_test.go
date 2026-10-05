package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/config"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/core/deps"
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

	cfg := &config.Config{
		RLS: &ratelimitstore.Config{
			CleanupInterval: time.Minute,
			Routes:          routes,
		},
	}

	return &deps.Dependencies{
		Cfg:    cfg,
		Logger: testLogger(),
		RLS:    rls,
	}
}

func defaultRateLimitRoutes() map[string]ratelimitstore.RateLimitConfig {
	return map[string]ratelimitstore.RateLimitConfig{
		"getorders": {Window: time.Minute, MaxRequests: 100},
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
		{http.MethodPost, "/api/goods"},
		{http.MethodPost, "/api/orders"},
		{http.MethodGet, "/api/orders/{number}"},
	}

	for _, rt := range want {
		assert.Containsf(t, got, rt, "ожидался маршрут %s %s", rt.method, rt.path)
	}
	assert.Len(t, got, len(want), "обнаружены лишние или отсутствующие маршруты: %+v", got)
}

func TestRegisterGoodsRoutes_NeverPanics(t *testing.T) {
	d := buildDeps(t, map[string]ratelimitstore.RateLimitConfig{})

	assert.NotPanics(t, func() {
		registerGoodsRoutes(chi.NewRouter(), d)
	})
}

func TestRegisterOrdersRoutes_PanicsWithoutGetOrdersConfig(t *testing.T) {
	d := buildDeps(t, map[string]ratelimitstore.RateLimitConfig{})

	assert.PanicsWithValue(t, "getorders rate limit config not found", func() {
		registerOrdersRoutes(chi.NewRouter(), d)
	})
}

func TestRegisterOrdersRoutes_OKWithConfigPresent(t *testing.T) {
	d := buildDeps(t, defaultRateLimitRoutes())

	assert.NotPanics(t, func() {
		registerOrdersRoutes(chi.NewRouter(), d)
	})
}
