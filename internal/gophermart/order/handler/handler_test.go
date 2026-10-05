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

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	ordersvc "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/service"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestHandler(t *testing.T) (*Handler, *MockOrderService) {
	t.Helper()

	svc := NewMockOrderService(t)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	return New(logger, svc), svc
}

// CreateOrder
func TestHandler_CreateOrder_Success(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		strings.NewReader("79927398713"),
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			"79927398713",
			uid,
		).
		Return(nil)

	h.CreateOrder(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
}

func TestHandler_CreateOrder_InvalidOrder(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		strings.NewReader("123"),
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			"123",
			uid,
		).
		Return(ordersvc.ErrInvalidOrderNum)

	h.CreateOrder(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestHandler_CreateOrder_OrderAlreadyCreatedByAnotherUser(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		strings.NewReader("79927398713"),
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			"79927398713",
			uid,
		).
		Return(ordersvc.ErrOrderAlreadyCreatedByAnotherUser)

	h.CreateOrder(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandler_CreateOrder_InternalError(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		strings.NewReader("79927398713"),
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			"79927398713",
			uid,
		).
		Return(errors.New("unknown error"))

	h.CreateOrder(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandler_CreateOrder_EmptyBody(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		nil,
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	h.CreateOrder(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_CreateOrder_BodyTooLarge(t *testing.T) {
	h, _ := newTestHandler(t)

	body := strings.Repeat("a", 1024)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/user/orders",
		strings.NewReader(body),
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	h.CreateOrder(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

}

// GetOrders
func TestHandler_GetOrders_Success(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/user/orders",
		nil,
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	orders := []model.Order{
		{
			OrderNum:   "79927398713",
			Status:     "NEW",
			UploadedAt: time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			OrderNum:   "12345678903",
			Status:     "PROCESSING",
			UploadedAt: time.Date(2025, 1, 11, 12, 0, 0, 0, time.UTC),
		},
	}

	svc.EXPECT().
		GetOrders(mock.Anything, uid).
		Return(orders, nil)

	h.GetOrders(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp []OrderResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp, 2)

	assert.Equal(t, "79927398713", resp[0].OrderNum)
	assert.Equal(t, "NEW", resp[0].Status)

	assert.Equal(t, "12345678903", resp[1].OrderNum)
	assert.Equal(t, "PROCESSING", resp[1].Status)
}

func TestHandler_GetOrders_NoOrders(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/user/orders",
		nil,
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		GetOrders(mock.Anything, uid).
		Return([]model.Order{}, nil)

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHandler_GetOrders_InternalError(t *testing.T) {
	h, svc := newTestHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/user/orders",
		nil,
	)

	uid := uuid.New()

	req = req.WithContext(
		ctxkeys.WithUserID(req.Context(), uid),
	)

	rec := httptest.NewRecorder()

	svc.EXPECT().
		GetOrders(mock.Anything, uid).
		Return(nil, errors.New("unknown error"))

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
