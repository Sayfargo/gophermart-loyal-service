// Package handler предоставляет HTTP-обработчики для управления заказами
// и обработки запросов на расчет баллов лояльности.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	ordersvc "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/service"
	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
)

// OrderService определяет интерфейс бизнес-логики для управления заказами.
// Этот интерфейс должен быть реализован сервисным слоем пакета orders.
type OrderService interface {
	// UploadOrder регистрирует новый заказ в системе для последующего расчета баллов.
	UploadOrder(ctx context.Context, order model.Order) error
	// GetOrder возвращает детальную информацию о заказе по его уникальному UUID-идентификатору.
	GetOrder(ctx context.Context, orderNum string) (model.Order, error)
}

// Handler инкапсулирует в себе структурированный логгер и сервисную логику
// для обработки входящих HTTP-запросов, связанных с заказами.
type Handler struct {
	logger  *slog.Logger
	service OrderService
}

// New создает и инициализирует новый экземпляр Handler с необходимыми зависимостями.
func New(
	log *slog.Logger,
	service OrderService,
) *Handler {
	return &Handler{
		logger:  log,
		service: service,
	}
}

// GoodRequest представляет входящую JSON-структуру для описания
// одной товарной позиции внутри запроса на создание заказа.
type GoodRequest struct {
	// Description содержит текстовое описание или наименование товара.
	Description string `json:"description"`
	// Price определяет стоимость единицы товара (число произвольной точности).
	Price decimal.Decimal `json:"price"`
}

// CreateOrderRequest описывает структуру тела входящего HTTP-запроса (JSON)
// для регистрации нового заказа, содержащего список товаров.
type CreateOrderRequest struct {
	// OrderNum содержит уникальный строковый номер заказа.
	OrderNum string `json:"order"`
	// Goods содержит перечень товарных позиций, входящих в этот заказ.
	Goods []GoodRequest `json:"goods"`
}

// GetOrderResponse описывает структуру JSON-ответа, возвращаемого
// при успешном запросе информации о конкретном заказе.
type GetOrderResponse struct {
	// Номер заказа
	Order string `json:"order"`
	// Текущий статус обработки заказа из accrual
	Status string `json:"status"`
	// Если расчёт был окончен - Accrual будет содержать количество баллов
	// Которые необходимы к начислению
	// В ином случае Accrual будет nil
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
}

const (
	orderNumParamKey = "number"
)

// GetOrder обрабатывает входящий HTTP-запрос на получение данных о заказе.
// Метод извлекает номер заказа из URL, запрашивает информацию у бизнес-логики и возвращает:
//   - 200 OK с деталями заказа в формате JSON, если заказ успешно найден;
//   - 204 No Content, если заказ с таким номером отсутствует в системе;
//   - 499 Status Request Client Closed, если клиент разорвал соединение до завершения обработки;
//   - 500 Internal Server Error при возникновении непредвиденных системных ошибок.
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderNum := chi.URLParam(r, orderNumParamKey)

	result, err := h.service.GetOrder(r.Context(), orderNum)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			h.logger.Debug("get order canceled by client")
			w.WriteHeader(499)
			return
		case errors.Is(err, ordersvc.ErrOrderNotFound):
			h.logger.Info(
				"order not found",
				"order", orderNum,
				"err", err,
			)
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			h.logger.Error(
				"unexpected error during get order",
				"err", err,
			)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

	}

	resp := GetOrderResponse{
		Order:   result.OrderNum,
		Status:  result.Status.String(),
		Accrual: result.Accrual,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error(
			"failed encode json",
			"err", err,
		)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

}

// CreateOrder обрабатывает HTTP-запрос на регистрацию нового заказа для расчета баллов лояльности.
// Декодирует JSON-тело, выполняет маппинг в доменную модель и транслирует ошибки бизнес-логики в HTTP-статусы:
//   - 202 StatusAccepted — заказ успешно принят в обработку.
//   - 400 Bad Request — невалидный формат JSON или некорректный номер заказа (ошибка Луна).
//   - 409 Conflict — заказ с таким номером уже обрабатывается или был обработан ранее.
//   - 499 Client Closed Request — обработка запроса была прервана клиентом.
//   - 500 Internal Server Error — непредвиденная внутренняя ошибка сервера.
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {

	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	order := req.toModel()

	if err := h.service.UploadOrder(r.Context(), order); err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			h.logger.Debug("create order canceled by client")
			w.WriteHeader(499)
			return
		case errors.Is(err, ordersvc.ErrInvalidOrderNum):
			h.logger.Info(
				"invalid order number",
				"order", req.OrderNum,
				"err", err,
			)
			http.Error(
				w,
				http.StatusText(http.StatusBadRequest),
				http.StatusBadRequest,
			)
			return
		case errors.Is(err, ordersvc.ErrOrderAlreadyProcessing):
			h.logger.Info(
				"order already uploaded by user",
				"err", err,
			)
			http.Error(
				w,
				http.StatusText(http.StatusConflict),
				http.StatusConflict,
			)
			return
		default:
			h.logger.Error(
				"unexpected error during order saving",
				"err", err,
			)

			http.Error(
				w,
				http.StatusText(http.StatusInternalServerError),
				http.StatusInternalServerError,
			)
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

func (r CreateOrderRequest) toModel() model.Order {
	goods := make([]model.Good, 0, len(r.Goods))

	for _, g := range r.Goods {
		goods = append(goods, model.Good{
			Description: g.Description,
			Price:       g.Price,
		})
	}

	return *model.NewOrder(r.OrderNum, goods)
}
