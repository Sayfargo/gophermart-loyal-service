// Package app является точкой сборки всего микросервиса (DI-контейнером).
// Отвечает за инициализацию пулов соединений, кэшей, бизнес-логики,
// маршрутизации, middleware и фоновых воркеров, собирая их в единое приложение.
package app

import (
	"context"
	"fmt"
	"log/slog"

	goodsh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/handler"
	goodsrepo "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/repository"
	goodssvc "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goodscache"
	ordersh "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/handler"
	ordersrepo "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/repository"
	orderssvc "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/worker"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/http/httpmiddleware"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/config"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/core/deps"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/transport/http/router"
	migrations "github.com/Sayfargo/gophermart-loyal-service/migrations/accrual"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/httpserver"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/postgres"
	"github.com/shopspring/decimal"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// App объединяет в себе все ключевые компоненты запущенного приложения,
// предоставляя интерфейс для его централизованного старта и остановки.
type App struct {
	// Server управляет жизненным циклом HTTP-сервера.
	Server *httpserver.HTTPServer
	// Pgxpool представляет собой пул соединений к СУБД PostgreSQL.
	Pgxpool *pgxpool.Pool
	// Worker отвечает за асинхронную фоновую обработку начисления баллов лояльности.
	Worker *worker.Worker
}

// New создает, связывает зависимости (Dependency Injection) и возвращает готовый к работе экземпляр App.
// Производит автоматический запуск миграций базы данных и предварительный прогрев (warm-up) кэша правил вознаграждений.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {

	// Отключаем кавычки при JSON-сериализации decimal.Decimal,
	// чтобы баланс отдавался числом (500.5), а не строкой ("500.5").
	decimal.MarshalJSONWithoutQuotes = true
	// Root router
	rootRouter := chi.NewRouter()

	pool, err := postgres.New(*cfg.DB, log, migrations.EmbedMigrations)
	if err != nil {
		log.Error("failed to create db connection pool", "err", err)
		return nil, fmt.Errorf("database initialize: %w", err)
	}

	httpServer := httpserver.New(rootRouter, cfg.Server, log)
	rateLimitStore := ratelimitstore.New(ctx, cfg.RLS, log)

	orderRepo := ordersrepo.New(pool)
	goodsRepo := goodsrepo.New(pool)

	// Загружаем все правила вознаграждений из базы данных и сохраняем их в кэш
	goodsCache := goodscache.New(goodsRepo)
	if err := goodsCache.Load(ctx); err != nil {
		log.Error("failed to load goods cache", "err", err)
		return nil, fmt.Errorf("load goods cache: %w", err)
	}
	// Создаём воркер для обработки заказов
	orderWorker := worker.New(orderRepo, goodsCache, log, cfg.W)

	orderSvc := orderssvc.New(orderRepo, log, orderWorker)
	goodsSvc := goodssvc.New(goodsRepo, goodsCache)

	orderHandler := ordersh.New(log, orderSvc)
	goodsHandler := goodsh.New(log, goodsSvc)

	dependencies := deps.New(
		orderHandler,
		goodsHandler,
		cfg,
		log,
		rateLimitStore,
	)

	if err := dependencies.Validate(); err != nil {
		return nil, fmt.Errorf("deps validate: %w", err)
	}

	rootRouter.Use(
		chimiddleware.Recoverer,
		httpmiddleware.RequestID(),
		httpmiddleware.Logging(log),
		httpmiddleware.GzipCompress(),
	)

	// Регистрируем все маршруты здесь
	router.SetupRoutes(rootRouter, dependencies)

	return &App{
		Server:  httpServer,
		Pgxpool: pool,
		Worker:  orderWorker,
	}, nil

}

// Run параллельно запускает фонового воркера обработки начислений и блокирует основной поток выполнения
// на время работы HTTP-сервера до момента получения сигнала остановки через ctx.
func (a *App) Run(ctx context.Context) error {

	go a.Worker.Run(ctx)

	if err := a.Server.Run(ctx); err != nil {
		return fmt.Errorf("server run: %w", err)
	}

	return nil
}
