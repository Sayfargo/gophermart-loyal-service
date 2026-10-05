package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	ordersvc "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/service"
	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func requestWithOrderParam(method, url, order string) *http.Request {
	req := httptest.NewRequest(method, url, nil)

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(orderNumParamKey, order)

	return req.WithContext(
		context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx),
	)
}

func TestHandler_GetOrder_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	accrual := decimal.RequireFromString("500.25")

	svc.EXPECT().
		GetOrder(mock.Anything, "12345678903").
		Return(model.Order{
			OrderNum: "12345678903",
			Status:   model.OrderStatusProcessed,
			Accrual:  &accrual,
		}, nil).
		Once()

	req := requestWithOrderParam(
		http.MethodGet,
		"/api/orders/12345678903",
		"12345678903",
	)

	rec := httptest.NewRecorder()

	h.GetOrder(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp GetOrderResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, "12345678903", resp.Order)
	assert.Equal(t, model.OrderStatusProcessed.String(), resp.Status)
	assert.True(t, accrual.Equal(*resp.Accrual))
}

func TestHandler_GetOrder_NotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	svc.EXPECT().
		GetOrder(mock.Anything, "123").
		Return(model.Order{}, ordersvc.ErrOrderNotFound).
		Once()

	req := requestWithOrderParam(
		http.MethodGet,
		"/api/orders/123",
		"123",
	)

	rec := httptest.NewRecorder()

	h.GetOrder(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandler_GetOrder_ContextCanceled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	svc.EXPECT().
		GetOrder(mock.Anything, "123").
		Return(model.Order{}, context.Canceled).
		Once()

	req := requestWithOrderParam(
		http.MethodGet,
		"/api/orders/123",
		"123",
	)

	rec := httptest.NewRecorder()

	h.GetOrder(rec, req)

	require.Equal(t, 499, rec.Code)
}

func TestHandler_GetOrder_InternalError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	svc.EXPECT().
		GetOrder(mock.Anything, "123").
		Return(model.Order{}, errors.New("db error")).
		Once()

	req := requestWithOrderParam(
		http.MethodGet,
		"/api/orders/123",
		"123",
	)

	rec := httptest.NewRecorder()

	h.GetOrder(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandler_CreateOrder_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	body := `{
		"order":"12345678903",
		"goods":[
			{
				"description":"iPhone",
				"price":1000
			}
		]
	}`

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			mock.MatchedBy(func(order model.Order) bool {
				return order.OrderNum == "12345678903" &&
					order.Status == model.OrderStatusRegistered &&
					len(order.Goods) == 1 &&
					order.Goods[0].Description == "iPhone" &&
					order.Goods[0].Price.Equal(decimal.RequireFromString("1000"))
			}),
		).
		Return(nil).
		Once()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/orders",
		strings.NewReader(body),
	)

	rec := httptest.NewRecorder()

	h.CreateOrder(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
}

func TestHandler_CreateOrder_InvalidJSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/orders",
		strings.NewReader("{"),
	)

	rec := httptest.NewRecorder()

	h.CreateOrder(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_CreateOrder_InvalidOrderNumber(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := NewMockOrderService(t)
	h := New(logger, svc)

	svc.EXPECT().
		UploadOrder(
			mock.Anything,
			mock.AnythingOfType("model.Order"),
		).
		Return(ordersvc.ErrInvalidOrderNum).
		Once()

	body := `{
		"order":"12345678903",
		"goods":[]
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/orders",
		strings.NewReader(body),
	)

	rec := httptest.NewRecorder()

	h.CreateOrder(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateOrderRequest_toModel(t *testing.T) {
	price := decimal.RequireFromString("100")

	req := CreateOrderRequest{
		OrderNum: "123",
		Goods: []GoodRequest{
			{
				Description: "Tea",
				Price:       price,
			},
		},
	}

	got := req.toModel()

	require.Equal(t, "123", got.OrderNum)
	require.Equal(t, model.OrderStatusRegistered, got.Status)
	require.Nil(t, got.Accrual)

	require.Len(t, got.Goods, 1)

	assert.Equal(t, "Tea", got.Goods[0].Description)
	assert.True(t, price.Equal(got.Goods[0].Price))
}
