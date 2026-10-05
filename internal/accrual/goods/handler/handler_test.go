package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"

	mock "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/handler/mock"
	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHandler_RegisterGoods_Success проверяет успешную регистрацию:
// валидный JSON, сервис вернул nil, хендлер отдал 200.
func TestHandler_RegisterGoods_Success(t *testing.T) {
	svc := mock.NewMockGoodsService(t)
	svc.On("RegisterGoods", testifymock.Anything, testifymock.Anything).Return(nil)

	h := New(newTestLogger(), svc)

	body := `{"match":"Bork","reward":10,"reward_type":"%"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goods", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.RegisterGoods(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandler_RegisterGoods_InvalidJSON проверяет, что невалидное тело
// возвращает 400, сервис не вызывается.
func TestHandler_RegisterGoods_InvalidJSON(t *testing.T) {
	svc := mock.NewMockGoodsService(t)
	h := New(newTestLogger(), svc)

	req := httptest.NewRequest(http.MethodPost, "/api/goods", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()

	h.RegisterGoods(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandler_RegisterGoods_InvalidGoods проверяет, что ErrInvalidGoods
// от сервиса превращается в 400.
func TestHandler_RegisterGoods_InvalidGoods(t *testing.T) {
	svc := mock.NewMockGoodsService(t)
	svc.On("RegisterGoods", testifymock.Anything, testifymock.Anything).
		Return(model.ErrInvalidGoods)

	h := New(newTestLogger(), svc)

	body := `{"match":"","reward":10,"reward_type":"%"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goods", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.RegisterGoods(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandler_RegisterGoods_MatchExists проверяет, что ErrMatchAlreadyExists
// превращается в 409.
func TestHandler_RegisterGoods_MatchExists(t *testing.T) {
	svc := mock.NewMockGoodsService(t)
	svc.On("RegisterGoods", testifymock.Anything, testifymock.Anything).
		Return(model.ErrMatchAlreadyExists)

	h := New(newTestLogger(), svc)

	body := `{"match":"Bork","reward":10,"reward_type":"%"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goods", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.RegisterGoods(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// TestHandler_RegisterGoods_InternalError проверяет, что неизвестная ошибка
// сервиса превращается в 500.
func TestHandler_RegisterGoods_InternalError(t *testing.T) {
	svc := mock.NewMockGoodsService(t)
	svc.On("RegisterGoods", testifymock.Anything, testifymock.Anything).
		Return(errors.New("boom"))

	h := New(newTestLogger(), svc)

	body := `{"match":"Bork","reward":10,"reward_type":"%"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goods", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.RegisterGoods(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
