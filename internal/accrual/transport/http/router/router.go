// Package router отвечает за централизованное конфигурирование маршрутов (эндпоинтов) API
// и связывание путей запросов с соответствующими HTTP-обработчиками и middleware.
package router

import (
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/core/deps"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/transport/http/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SetupRoutes регистрирует все основные группы маршрутов приложения в корневом роутере chi.Router.
// Разделяет эндпоинты на логические блоки и пробрасывает в них
// инициализированный контейнер зависимостей deps.
func SetupRoutes(r chi.Router, deps *deps.Dependencies) {
	r.Route("/api/goods", func(r chi.Router) {
		registerGoodsRoutes(r, deps)
	})

	r.Route("/api/orders", func(r chi.Router) {
		registerOrdersRoutes(r, deps)
	})
}

// Метод для регистрации маршрутов для сервиса order

func registerOrdersRoutes(r chi.Router, deps *deps.Dependencies) {
	r.With(
		chimiddleware.AllowContentType("application/json"),
	).Post("/", deps.OrdersHandler.CreateOrder)

	getorderCfg, ok := deps.Cfg.RLS.Routes["getorders"]
	if !ok {
		panic("getorders rate limit config not found")
	}

	r.With(
		chimiddleware.AllowContentType("application/json"),
		middleware.GlobalRateLimit(deps.RLS, getorderCfg, "getorders"),
	).Get("/{number}", deps.OrdersHandler.GetOrder)
}

// Метод для регистрации маршрутов сервиса user

func registerGoodsRoutes(r chi.Router, deps *deps.Dependencies) {
	r.With(
		chimiddleware.AllowContentType("application/json"),
	).Post("/", deps.GoodsHandler.RegisterGoods)
}
