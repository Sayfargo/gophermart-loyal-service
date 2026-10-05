package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"
)

func init() {
	// decimal в JSON — числом, а не строкой.
	decimal.MarshalJSONWithoutQuotes = true
}

// mustDecimal парсит строку в decimal.Decimal, паникует при ошибке.
func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// newTestLogger возвращает logger, который пишет в никуда.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHandler_GetBalance_Success проверяет успешный сценарий:
// userID есть в контексте, сервис вернул баланс, хендлер отдал 200 и JSON.
func TestHandler_GetBalance_Success(t *testing.T) {
	userID := uuid.New()
	expected := model.Balance{
		UserID:    userID,
		Current:   mustDecimal("500.5"),
		Withdrawn: mustDecimal("42"),
	}

	svc := NewMockBalanceService(t)
	svc.On("GetBalance", mock.Anything, userID).Return(expected, nil)

	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got model.Balance
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.True(t, expected.Current.Equal(got.Current), "current mismatch")
	assert.True(t, expected.Withdrawn.Equal(got.Withdrawn), "withdrawn mismatch")
	svc.AssertExpectations(t)
}

// TestHandler_GetBalance_NoUserID проверяет, что без userID в контексте
// хендлер возвращает 401 Unauthorized.
func TestHandler_GetBalance_NoUserID(t *testing.T) {
	svc := NewMockBalanceService(t)
	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestHandler_GetBalance_ServiceError проверяет, что любая ошибка сервиса
// превращается в 500 Internal Server Error.
func TestHandler_GetBalance_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("GetBalance", mock.Anything, userID).
		Return(model.Balance{}, errors.New("boom"))

	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// TestHandler_Withdraw_Success проверяет успешный сценарий:
// валидный заказ и сумма, сервис вернул nil, хендлер отдал 200.
func TestHandler_Withdraw_Success(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("Withdraw", mock.Anything, userID, "12345678903", mustDecimal("100")).
		Return(nil)

	h := New(newTestLogger(), svc)

	body := `{"order":"12345678903","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

// TestHandler_Withdraw_NoUserID проверяет, что без userID в контексте
// хендлер возвращает 401 Unauthorized и не вызывает сервис.
func TestHandler_Withdraw_NoUserID(t *testing.T) {
	svc := NewMockBalanceService(t)
	h := New(newTestLogger(), svc)

	body := `{"order":"12345678903","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestHandler_Withdraw_InvalidJSON проверяет, что невалидное тело запроса
// возвращает 400 Bad Request.
func TestHandler_Withdraw_InvalidJSON(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`not json`))
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandler_Withdraw_InvalidOrder проверяет, что неверный номер заказа
// (не прошедший Луна) возвращает 422 Unprocessable Entity.
func TestHandler_Withdraw_InvalidOrder(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("Withdraw", mock.Anything, userID, "12345678904", mock.Anything).
		Return(model.ErrInvalidOrderNumber)

	h := New(newTestLogger(), svc)

	body := `{"order":"12345678904","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	svc.AssertExpectations(t)
}

// TestHandler_Withdraw_InsufficientFunds проверяет, что недостаток средств
// на балансе возвращает 402 Payment Required.
func TestHandler_Withdraw_InsufficientFunds(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("Withdraw", mock.Anything, userID, "12345678903", mock.Anything).
		Return(model.ErrInsufficientFunds)

	h := New(newTestLogger(), svc)

	body := `{"order":"12345678903","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	svc.AssertExpectations(t)
}

// TestHandler_Withdraw_OrderAlreadyUsed проверяет, что повторное использование
// заказа для списания возвращает 409 Conflict.
func TestHandler_Withdraw_OrderAlreadyUsed(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("Withdraw", mock.Anything, userID, "12345678903", mock.Anything).
		Return(model.ErrOrderAlreadyUsed)

	h := New(newTestLogger(), svc)

	body := `{"order":"12345678903","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	svc.AssertExpectations(t)
}

// TestHandler_Withdraw_InternalError проверяет, что любая неизвестная ошибка
// сервиса превращается в 500 Internal Server Error.
func TestHandler_Withdraw_InternalError(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("Withdraw", mock.Anything, userID, "12345678903", mock.Anything).
		Return(errors.New("unexpected failure"))

	h := New(newTestLogger(), svc)

	body := `{"order":"12345678903","sum":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(body))
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraw(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

func TestHandler_GetWithdrawals_Success(t *testing.T) {
	userID := uuid.New()
	expected := []model.Withdrawal{
		{Order: "12345678903", Sum: mustDecimal("100"), ProcessedAt: time.Now()},
		{Order: "79927398713", Sum: mustDecimal("50"), ProcessedAt: time.Now()},
	}

	svc := NewMockBalanceService(t)
	svc.On("GetWithdrawals", mock.Anything, userID).Return(expected, nil)

	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraws(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got []model.Withdrawal
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got, 2)
	assert.Equal(t, "12345678903", got[0].Order)
	svc.AssertExpectations(t)
}

func TestHandler_GetWithdrawals_Empty(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("GetWithdrawals", mock.Anything, userID).Return(nil, nil)

	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraws(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	svc.AssertExpectations(t)
}

func TestHandler_GetWithdrawals_NoUserID(t *testing.T) {
	svc := NewMockBalanceService(t)
	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	rec := httptest.NewRecorder()

	h.Withdraws(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_GetWithdrawals_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := NewMockBalanceService(t)
	svc.On("GetWithdrawals", mock.Anything, userID).Return(nil, errors.New("boom"))

	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	req = req.WithContext(ctxkeys.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Withdraws(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}
