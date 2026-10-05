package deps

import (
	"log/slog"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/config"
	goodsh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/handler"
	ordersh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/handler"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/stretchr/testify/require"
)

func validDependencies() *Dependencies {
	return &Dependencies{
		OrdersHandler: &ordersh.Handler{},
		GoodsHandler:  &goodsh.Handler{},
		Cfg:           &config.Config{},
		Logger:        slog.Default(),
		RLS:           &ratelimitstore.RateLimitStorage{},
	}
}

func TestDependencies_Validate(t *testing.T) {
	tests := []struct {
		name string
		deps *Dependencies
	}{
		{
			name: "valid dependencies",
			deps: validDependencies(),
		},
		{
			name: "nil dependencies",
			deps: nil,
		},
		{
			name: "nil orders handler",
			deps: func() *Dependencies {
				d := validDependencies()
				d.OrdersHandler = nil
				return d
			}(),
		},
		{
			name: "nil goods handler",
			deps: func() *Dependencies {
				d := validDependencies()
				d.GoodsHandler = nil
				return d
			}(),
		},
		{
			name: "nil config",
			deps: func() *Dependencies {
				d := validDependencies()
				d.Cfg = nil
				return d
			}(),
		},
		{
			name: "nil logger",
			deps: func() *Dependencies {
				d := validDependencies()
				d.Logger = nil
				return d
			}(),
		},
		{
			name: "nil rate limit storage",
			deps: func() *Dependencies {
				d := validDependencies()
				d.RLS = nil
				return d
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.deps.Validate()

			if tt.name == "valid dependencies" {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, ErrNilDependency)
		})
	}
}
