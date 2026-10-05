// Package app представляет собой центральный DI-контейнер и точку сборки микросервиса gophermart.
// Отвечает за инициализацию пула соединений PostgreSQL, криптографических компонентов (Argon2, JWT),
// клиента интеграции с системой начислений, маршрутизатора, middleware и фоновых воркеров.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/accrualclient"
	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/config"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/core/deps"
	orderh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/handler"
	orderrepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/repository"
	ordersvc "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/http/router"
	userh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/handler"
	userrepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/repository"
	usersvc "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/worker"
	gophermartMigrations "github.com/Sayfargo/gophermart-loyal-service/migrations/gophermart"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/http/httpmiddleware"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/httpserver"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/passhasher"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/postgres"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/shopspring/decimal"

	balanceh "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/handler"
	balancerepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/repository"
	balancesvc "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/service"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// App инкапсулирует в себе все ключевые компоненты запущенного приложения gophermart,
// предоставляя унифицированный интерфейс для управления его жизненным циклом.
type App struct {
	// Server управляет запуском и graceful shutdown HTTP-сервера.
	Server *httpserver.HTTPServer
	// Pgxpool представляет пул соединений с базой данных PostgreSQL.
	Pgxpool *pgxpool.Pool
	// Worker отвечает за асинхронный опрос и обновление статусов заказов во внешней системе accrual.
	Worker *worker.Worker
}

const (
	defaultJWTKey = "super-secret-key"
)

// New создает, связывает зависимости (Dependency Injection) и возвращает готовый к запуску экземпляр App.
// Метод автоматически применяет миграции базы данных gophermart при старте.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {

	// Отключаем кавычки при JSON-сериализации decimal.Decimal,
	// чтобы баланс отдавался числом (500.5), а не строкой ("500.5").
	decimal.MarshalJSONWithoutQuotes = true
	// Root router
	rootRouter := chi.NewRouter()

	pool, err := postgres.New(*cfg.DB, log, gophermartMigrations.EmbedMigrations)
	if err != nil {
		log.Error("failed to create db connection pool", "err", err)
		return nil, fmt.Errorf("database initialize: %w", err)
	}

	// Создаём HTTP-сервер и accrual client
	httpServer := httpserver.New(rootRouter, cfg.Server, log)
	accrualClient := accrualclient.New(*cfg.Accrual)

	// Создаём rate limiter store
	limiter := ratelimitstore.New(ctx, cfg.RLS, log)

	var (
		jwtSecret string
	)
	// Получаем секретный ключ для JWT из переменной окружения S_JWT
	jwtSecret = os.Getenv("S_JWT")

	if jwtSecret == "" {
		jwtSecret = defaultJWTKey
	}
	// Создаём сервис для работы с JWT
	tokenSvc := authentication.New([]byte(jwtSecret))
	passHasher := new(passhasher.Argon2Hasher)

	// repositories, services, handlers
	userRepo := userrepo.New(pool)
	orderRepo := orderrepo.New(pool)
	balanceRepo := balancerepo.New(pool)

	txBeginner := postgres.NewTxBeginner(pool)
	orderWorker := worker.New(orderRepo, balanceRepo, accrualClient, txBeginner, log, cfg.W)

	userSvc := usersvc.New(userRepo, tokenSvc, passHasher)
	orderSvc := ordersvc.New(orderRepo, log, orderWorker) // orderWorker реализует OrderNotifier.Notify
	balanceSvc := balancesvc.New(balanceRepo)

	userHandler := userh.New(log, userSvc)
	orderHandler := orderh.New(log, orderSvc)
	balanceHandler := balanceh.New(log, balanceSvc)

	// Создаём зависимости для передачи в router.SetupRoutes
	dependencies := deps.New(
		userHandler,
		orderHandler,
		balanceHandler,
		tokenSvc,
		limiter,
		cfg,
		log,
	)

	if err := dependencies.Validate(); err != nil {
		return nil, fmt.Errorf("deps validate: %w", err)
	}
	// Регистрируем middleware для rootRouter
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

// Run асинхронно запускает фонового воркера для обновления статусов заказов
// и блокирует текущую горутину на время работы HTTP-сервера до получения сигнала отмены ctx.
func (a *App) Run(ctx context.Context) error {

	go a.Worker.Run(ctx)

	if err := a.Server.Run(ctx); err != nil {
		return fmt.Errorf("server run: %w", err)
	}

	return nil
}
