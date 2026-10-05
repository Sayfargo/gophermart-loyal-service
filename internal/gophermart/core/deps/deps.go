// Package deps предоставляет контейнер зависимостей,
// используемый для передачи инициализированных слоев и обработчиков в подсистему маршрутизации gophermart.
package deps

import (
	"errors"
	"log/slog"

	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	balansh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/handler"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/config"
	orderh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/handler"
	userh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/handler"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
)

var (
	// ErrNilDependency означает, что обязательная зависимость контейнера не была инициализирована.
	ErrNilDependency = errors.New("dependency is required")
)

// Dependencies агрегирует в себе все HTTP-обработчики (handlers), сервисы аутентификации,
// настройки конфигурации, логгер и хранилище лимитов, необходимые для сборки API-маршрутов.
type Dependencies struct {
	// UserHandler отвечает за обработку HTTP-запросов аутентификации и регистрации пользователей.
	UserHandler *userh.Handler
	// OrderHandler отвечает за обработку HTTP-запросов загрузки и получения статуса заказов.
	OrderHandler *orderh.Handler
	// BalanceHandler отвечает за обработку HTTP-запросов проверки баланса и списания баллов.
	BalanceHandler *balansh.Handler
	// TokenSvc предоставляет логику генерации и валидации сессионных JWT-токенов.
	TokenSvc *authentication.TokenService
	// RLS предоставляет доступ к хранилищу лимитов частоты запросов.
	RLS *ratelimitstore.RateLimitStorage
	// Cfg хранит глобальные конфигурационные параметры сервиса gophermart.
	Cfg *config.Config
	// Logger предоставляет экземпляр структурированного логгера для трассировки запросов.
	Logger *slog.Logger
}

// New создает и возвращает новый заполненный экземпляр Dependencies,
// инкапсулируя переданные компоненты приложения в единый неизменяемый объект.
func New(
	userHandler *userh.Handler,
	orderHandler *orderh.Handler,
	balanceHandler *balansh.Handler,
	tokenSvc *authentication.TokenService,
	rls *ratelimitstore.RateLimitStorage,
	cfg *config.Config,
	logger *slog.Logger,
) *Dependencies {
	return &Dependencies{
		UserHandler:    userHandler,
		OrderHandler:   orderHandler,
		BalanceHandler: balanceHandler,
		TokenSvc:       tokenSvc,
		RLS:            rls,
		Cfg:            cfg,
		Logger:         logger,
	}
}

// Validate проверяет, что все обязательные зависимости контейнера инициализированы.
//
// Метод предотвращает запуск приложения с неполностью собранным контейнером зависимостей.
func (d *Dependencies) Validate() error {
	if d == nil {
		return ErrNilDependency
	}

	if d.UserHandler == nil {
		return ErrNilDependency
	}

	if d.OrderHandler == nil {
		return ErrNilDependency
	}

	if d.BalanceHandler == nil {
		return ErrNilDependency
	}

	if d.TokenSvc == nil {
		return ErrNilDependency
	}

	if d.RLS == nil {
		return ErrNilDependency
	}

	if d.Cfg == nil {
		return ErrNilDependency
	}

	if d.Logger == nil {
		return ErrNilDependency
	}

	return nil
}
