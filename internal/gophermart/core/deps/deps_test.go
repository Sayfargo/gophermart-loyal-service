package deps

import (
	"log/slog"
	"testing"

	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	balansh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/handler"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/config"
	orderh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/handler"
	userh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/handler"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/stretchr/testify/require"
)

func validDependencies() *Dependencies {
	return &Dependencies{
		UserHandler:    &userh.Handler{},
		OrderHandler:   &orderh.Handler{},
		BalanceHandler: &balansh.Handler{},
		TokenSvc:       &authentication.TokenService{},
		RLS:            &ratelimitstore.RateLimitStorage{},
		Cfg:            &config.Config{},
		Logger:         slog.Default(),
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
			name: "nil user handler",
			deps: func() *Dependencies {
				d := validDependencies()
				d.UserHandler = nil
				return d
			}(),
		},
		{
			name: "nil order handler",
			deps: func() *Dependencies {
				d := validDependencies()
				d.OrderHandler = nil
				return d
			}(),
		},
		{
			name: "nil balance handler",
			deps: func() *Dependencies {
				d := validDependencies()
				d.BalanceHandler = nil
				return d
			}(),
		},
		{
			name: "nil token service",
			deps: func() *Dependencies {
				d := validDependencies()
				d.TokenSvc = nil
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
