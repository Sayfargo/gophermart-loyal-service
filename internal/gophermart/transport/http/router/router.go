// Package router отвечает за централизованное конфигурирование маршрутов (эндпоинтов) API
// и связывание путей запросов с соответствующими HTTP-обработчиками и middleware.
package router

import (
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/core/deps"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/http/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SetupRoutes регистрирует все основные группы маршрутов приложения в корневом роутере chi.Router.
// Разделяет эндпоинты на логические блоки пробрасывает в них
// инициализированный контейнер зависимостей deps.
func SetupRoutes(r chi.Router, deps *deps.Dependencies) {
	r.Route("/api/user", func(r chi.Router) {
		registerUserRoutes(r, deps)

		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthMiddleware(deps.TokenSvc))

			r.Route("/orders", func(r chi.Router) {
				registerOrderRoutes(r, deps)
			})

			registerBalanceRoutes(r, deps)
		})
	})
}

// Метод для регистрации маршрутов сервиса user

func registerUserRoutes(r chi.Router, deps *deps.Dependencies) {

	regCfg, ok := deps.Cfg.RLS.Routes["register"]
	if !ok {
		panic("register rate limit config not found")
	}

	logCfg, ok := deps.Cfg.RLS.Routes["login"]
	if !ok {
		panic("login rate limit config not found")
	}

	r.With(
		chimiddleware.AllowContentType("application/json"),
		middleware.RateLimit(deps.RLS, regCfg, "register"),
	).Post("/register", deps.UserHandler.RegisterUser)

	r.With(
		chimiddleware.AllowContentType("application/json"),
		middleware.RateLimit(deps.RLS, logCfg, "login"),
	).Post("/login", deps.UserHandler.LoginUser)
}

// Метод для регистрации маршрутов для сервиса order

func registerOrderRoutes(r chi.Router, deps *deps.Dependencies) {
	r.Post("/", deps.OrderHandler.CreateOrder)

	r.Get("/", deps.OrderHandler.GetOrders)
}

func registerBalanceRoutes(r chi.Router, deps *deps.Dependencies) {
	r.Get("/balance", deps.BalanceHandler.GetBalance)
	r.Post("/balance/withdraw", deps.BalanceHandler.Withdraw)
	r.Get("/withdrawals", deps.BalanceHandler.Withdraws)
}
