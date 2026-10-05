// Package handler предоставляет HTTP-обработчики для регистрации новых заказов пользователей
// и получения истории загруженных заказов с актуальными статусами их обработки.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	ordersvc "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderService определяет интерфейс бизнес-логики для управления заказами пользователей,
// который должен быть реализован сервисным слоем.
type OrderService interface {
	// UploadOrder регистрирует новый номер заказа в системе для последующего расчета баллов.
	UploadOrder(ctx context.Context, orderNumStr string, uid uuid.UUID) error
	// GetOrders извлекает полный перечень всех заказов, загруженных конкретным пользователем.
	GetOrders(ctx context.Context, uid uuid.UUID) ([]model.Order, error)
}

// Handler инкапсулирует структурированный логгер и интерфейс сервисного слоя
// для обработки входящих HTTP-запросов подсистемы заказов.
type Handler struct {
	l   *slog.Logger
	svc OrderService
}

// New создает и инициализирует новый экземпляр Handler с необходимыми внешними зависимостями.
func New(
	log *slog.Logger,
	service OrderService,
) *Handler {
	return &Handler{
		l:   log,
		svc: service,
	}
}

// OrderResponse описывает структуру JSON-ответа при выгрузке истории заказов клиента.
type OrderResponse struct {
	// OrderNum содержит уникальный номер заказа.
	OrderNum string `json:"number"`
	// Status отражает текстовое представление текущего этапа обработки (NEW, PROCESSING и т.д.).
	Status string `json:"status"`
	// Accrual определяет сумму начисленных баллов (поле отсутствует в JSON, если расчет не завершен).
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
	// UploadedAt фиксирует точное время добавления заказа в систему.
	UploadedAt time.Time `json:"uploaded_at"`
}

const maxBodySize = 32

// GetOrders обрабатывает HTTP-запрос на получение истории всех загруженных заказов авторизованного пользователя.
// Идентификатор пользователя автоматически извлекается из контекста запроса.
// Возвращаемые HTTP-статусы:
//   - 200 OK — история успешно найдена и возвращена в виде JSON-массива.
//   - 204 No Content — у пользователя еще нет ни одного зарегистрированного заказа.
//   - 499 Client Closed Request — обработка запроса была прервана клиентом на этапе ожидания данных.
//   - 500 Internal Server Error — непредвиденная ошибка контекста или СУБД.
func (h *Handler) GetOrders(w http.ResponseWriter, r *http.Request) {
	uid, err := ctxkeys.GetUserID(r.Context())
	if err != nil {
		h.l.Error(
			"failed to get user ID from context",
			"err", err,
		)
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	orders, err := h.svc.GetOrders(r.Context(), uid)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			h.l.Debug("create order canceled by client")
			w.WriteHeader(499)
			return
		default:
			h.l.Error(
				"unexpected error during orders loading",
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

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := make([]OrderResponse, len(orders))

	for i, o := range orders {
		resp[i] = toOrderResponse(o)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.l.Error(
			"failed to marshal response json",
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

// CreateOrder обрабатывает HTTP-запрос на регистрацию нового заказа в системе лояльности.
// Номер заказа принимается в виде необработанной текстовой строки (text/plain) в теле запроса.
// Метод включает встроенное ограничение размера тела (MaxBytesReader) для защиты от DoS-атак.
// Возвращаемые HTTP-статусы:
//   - 202 StatusAccepted — новый номер заказа успешно принят в обработку.
//   - 200 OK — заказ уже был загружен этим пользователем ранее.
//   - 400 Bad Request — пустое тело запроса или ошибка чтения данных.
//   - 409 Conflict — этот номер заказа уже заведен в систему другим пользователем.
//   - 422 Unprocessable Entity — невалидный номер заказа (ошибка контрольной суммы Луна).
//   - 499 Client Closed Request — выполнение операции прервано клиентом.
//   - 500 Internal Server Error — внутренняя ошибка авторизационного контекста или СУБД.
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {

	// Защита от DDos атак если отправляют запрос с телом размера в гигабайты
	body := http.MaxBytesReader(
		w,
		r.Body,
		maxBodySize,
	)

	data, err := io.ReadAll(body)
	if err != nil || len(data) == 0 {
		h.l.Info(
			"failed to read body",
			"err", err,
		)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	uidStr, err := ctxkeys.GetUserID(r.Context())
	if err != nil {
		h.l.Error(
			"failed to get user ID from context",
			"err", err,
		)
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	orderNumStr := strings.TrimSpace(string(data))

	if err := h.svc.UploadOrder(r.Context(), orderNumStr, uidStr); err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			h.l.Debug("create short url canceled by client")
			w.WriteHeader(499)
			return
		case errors.Is(err, ordersvc.ErrInvalidOrderNum):
			h.l.Info(
				"invalid order number",
				"order", orderNumStr,
				"err", err,
			)
			http.Error(
				w,
				http.StatusText(http.StatusUnprocessableEntity),
				http.StatusUnprocessableEntity,
			)
			return
		case errors.Is(err, ordersvc.ErrOrderAlreadyCreatedByAnotherUser):
			h.l.Info(
				"order already uploaded by another user",
				"err", err,
			)
			http.Error(
				w,
				http.StatusText(http.StatusConflict),
				http.StatusConflict,
			)
			return
		case errors.Is(err, ordersvc.ErrOrderAlreadyProcessing):
			h.l.Info(
				"order has already been uploaded by this user",
				"err", err,
			)
			w.WriteHeader(http.StatusOK)
			return
		default:
			h.l.Error(
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

func toOrderResponse(o model.Order) OrderResponse {
	return OrderResponse{
		OrderNum:   o.OrderNum,
		Status:     o.Status.String(),
		Accrual:    o.Accrual,
		UploadedAt: o.UploadedAt,
	}
}
