// Package handler предоставляет HTTP-обработчики для проверки текущего баланса пользователя,
// оформления запросов на списание баллов и получения истории всех выводов средств.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BalanceService определяет интерфейс бизнес-логики для управления счетами и транзакциями пользователей.
type BalanceService interface {
	// GetBalance возвращает информацию о текущем балансе и общей сумме списаний пользователя.
	GetBalance(ctx context.Context, userID uuid.UUID) (model.Balance, error)
	// Withdraw списывает баллы со счета пользователя в счет оплаты указанного заказа.
	Withdraw(ctx context.Context, userID uuid.UUID, orderNum string, sum decimal.Decimal) error
	// GetWithdrawals возвращает полную историю успешных списаний баллов для конкретного пользователя.
	GetWithdrawals(ctx context.Context, userID uuid.UUID) ([]model.Withdrawal, error)
}

// Handler инкапсулирует структурированный логгер и интерфейс BalanceService для обработки входящих HTTP-запросов.
type Handler struct {
	logger  *slog.Logger
	service BalanceService
}

// New создает и инициализирует новый экземпляр Handler для работы с подсистемой баланса.
func New(logger *slog.Logger, balanceService BalanceService) *Handler {
	return &Handler{
		logger:  logger,
		service: balanceService,
	}
}

// GetBalance обрабатывает HTTP-запрос на получение текущего состояния счета аутентифицированного пользователя.
// Возвращает JSON-структуру с доступным балансом и накопленным списанием:
//   - 200 OK — успешный возврат баланса в формате JSON.
//   - 401 Unauthorized — идентификатор пользователя отсутствует в контексте запроса.
//   - 500 Internal Server Error — непредвиденная ошибка на стороне хранилища.
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID, err := ctxkeys.GetUserID(r.Context())
	if err != nil {
		h.logger.Error("get user id from context failed", "err", err)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	balance, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		h.logger.Error("get balance failed", "err", err, "user_id", userID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(balance); err != nil {
		h.logger.Error("encode balance failed", "err", err)
	}
}

// Withdraw обрабатывает HTTP-запрос на списание баллов лояльности в счет оплаты нового заказа.
// Декодирует сумму и номер заказа из JSON-тела и транслирует ошибки бизнес-логики в HTTP-статусы:
//   - 200 OK — успешное списание средств со счета.
//   - 400 Bad Request — невалидная структура входящего JSON-запроса.
//   - 401 Unauthorized — пользователь не авторизован (отсутствует ID в контексте).
//   - 402 Payment Required — на счете пользователя недостаточно средств для списания.
//   - 409 Conflict — данный номер заказа уже использовался для списания средств ранее.
//   - 422 Unprocessable Entity — переданный номер заказа не прошел валидацию контрольной суммы (алгоритм Луна).
//   - 500 Internal Server Error — внутренняя ошибка выполнения транзакции СУБД.
func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID, err := ctxkeys.GetUserID(r.Context())
	if err != nil {
		h.logger.Error("get user id from context failed", "err", err)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	var req model.WithdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	err = h.service.Withdraw(r.Context(), userID, req.Order, req.Sum)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, model.ErrInvalidOrderNumber):
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
	case errors.Is(err, model.ErrInsufficientFunds):
		http.Error(w, http.StatusText(http.StatusPaymentRequired), http.StatusPaymentRequired)
	case errors.Is(err, model.ErrOrderAlreadyUsed):
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
	default:
		h.logger.Error("withdraw failed", "err", err, "user_id", userID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// Withdraws обрабатывает HTTP-запрос на получение списка всех прошлых успешных списаний баллов пользователем.
// Ответ сортируется по дате (в соответствии с реализацией репозитория):
//   - 200 OK — успешный возврат истории транзакций в виде JSON-массива.
//   - 204 NoContent — у пользователя отсутствуют операции списания.
//   - 401 Unauthorized — пользователь не идентифицирован.
//   - 500 Internal Server Error — непредвиденная ошибка чтения истории из базы данных.
func (h *Handler) Withdraws(w http.ResponseWriter, r *http.Request) {
	userID, err := ctxkeys.GetUserID(r.Context())
	if err != nil {
		h.logger.Error("get user id from context failed", "err", err)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	withdrawals, err := h.service.GetWithdrawals(r.Context(), userID)
	if err != nil {
		h.logger.Error("get withdrawals failed", "err", err, "user_id", userID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(withdrawals); err != nil {
		h.logger.Error("encode withdrawals failed", "err", err)
	}
}
