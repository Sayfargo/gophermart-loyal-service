// Package deps предоставляет контейнер зависимостей,
// используемый для удобной передачи инициализированных слоев и компонентов в подсистему маршрутизации.
package deps

import (
	"errors"
	"log/slog"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/config"
	goodsh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/handler"
	ordersh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/handler"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
)

var (
	// ErrNilDependency означает, что обязательная зависимость контейнера не была инициализирована.
	ErrNilDependency = errors.New("dependency is required")
)

// Dependencies объединяет в себе все HTTP-обработчики (handlers), настройки конфигурации,
// логгер и хранилище лимитов, необходимые для конфигурирования эндпоинтов API.
type Dependencies struct {
	// OrdersHandler отвечает за обработку входящих HTTP-запросов, связанных с заказами.
	OrdersHandler *ordersh.Handler
	// GoodsHandler отвечает за обработку входящих HTTP-запросов, связанных с товарами и правилами начисления.
	GoodsHandler *goodsh.Handler
	// Cfg хранит глобальную конфигурацию приложения.
	Cfg *config.Config
	// Logger предоставляет доступ к структурированному логированию.
	Logger *slog.Logger
	// RLS предоставляет доступ к хранилищу состояний лимитов запросов.
	RLS *ratelimitstore.RateLimitStorage
}

// New создает и возвращает новый заполненный экземпляр Dependencies,
// инкапсулируя все переданные слои приложения в единый объект.
func New(
	ordersHandler *ordersh.Handler,
	goodsHandler *goodsh.Handler,
	cfg *config.Config,
	logger *slog.Logger,
	rls *ratelimitstore.RateLimitStorage,
) *Dependencies {
	return &Dependencies{
		OrdersHandler: ordersHandler,
		GoodsHandler:  goodsHandler,
		Cfg:           cfg,
		Logger:        logger,
		RLS:           rls,
	}
}

// Validate проверяет, что все обязательные зависимости приложения инициализированы.
//
// Метод предотвращает запуск приложения с неполностью собранным контейнером зависимостей.
func (d *Dependencies) Validate() error {
	if d == nil {
		return ErrNilDependency
	}

	if d.OrdersHandler == nil {
		return ErrNilDependency
	}

	if d.GoodsHandler == nil {
		return ErrNilDependency
	}

	if d.Cfg == nil {
		return ErrNilDependency
	}

	if d.Logger == nil {
		return ErrNilDependency
	}

	if d.RLS == nil {
		return ErrNilDependency
	}

	return nil
}
