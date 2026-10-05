// Package handler предоставляет HTTP-обработчики для управления товарами
// и правилами начисления баллов лояльности.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
)

// GoodsService определяет интерфейс бизнес-логики для работы с товарами,
// который должен быть реализован сервисным слоем.
type GoodsService interface {
	// RegisterGoods выполняет бизнес-валидацию и регистрацию нового товара или правила.
	RegisterGoods(ctx context.Context, goods model.GoodsInfo) error
}

// Handler инкапсулирует в себе логгер и сервисную логику для обработки
// входящих HTTP-запросов, связанных с товарами.
type Handler struct {
	logger  *slog.Logger
	service GoodsService
}

// New создает и инициализирует новый экземпляр Handler с необходимыми зависимостями.
func New(logger *slog.Logger, goodsService GoodsService) *Handler {
	return &Handler{
		logger:  logger,
		service: goodsService,
	}
}

// RegisterGoods обрабатывает HTTP-запрос на регистрацию нового товара или правила начисления.
// Декодирует JSON из тела запроса и транслирует ошибки бизнес-логики в соответствующие HTTP-статусы:
//   - 200 OK — успешная регистрация.
//   - 400 Bad Request — невалидный JSON или некорректные данные товара.
//   - 409 Conflict — правило для данного товара уже существует.
//   - 500 Internal Server Error — непредвиденная ошибка на стороне сервера.
func (h *Handler) RegisterGoods(w http.ResponseWriter, r *http.Request) {

	var req model.GoodsInfo
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	err := h.service.RegisterGoods(r.Context(), req)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, model.ErrInvalidGoods):
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	case errors.Is(err, model.ErrMatchAlreadyExists):
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
	default:
		h.logger.Error("register goods failed", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

}
