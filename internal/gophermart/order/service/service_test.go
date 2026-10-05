package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	orderrepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) (*OrderService, *MockOrderRepository, *MockOrderNotifier) {
	t.Helper()

	repo := NewMockOrderRepository(t)
	notifier := NewMockOrderNotifier(t)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	return New(repo, logger, notifier), repo, notifier
}

// UploadOrder
func TestService_UploadOrder_Success(t *testing.T) {
	svc, repo, notif := newTestService(t)

	uid := uuid.New()
	orderNumber := "79927398713"

	repo.EXPECT().
		CreateOrder(
			mock.Anything,
			mock.AnythingOfType("*model.Order"),
		).
		Return(nil)

	notif.EXPECT().
		Notify(mock.AnythingOfType("model.Order"))

	err := svc.UploadOrder(
		context.Background(),
		orderNumber,
		uid,
	)

	require.NoError(t, err)
}

func TestService_UploadOrder_InvalidOrder(t *testing.T) {
	svc, _, _ := newTestService(t)

	err := svc.UploadOrder(
		context.Background(),
		"abc",
		uuid.New(),
	)

	require.ErrorIs(t, err, ErrInvalidOrderNum)
}

func TestService_UploadOrder_OrderAlreadyProcessing(t *testing.T) {
	svc, repo, _ := newTestService(t)

	repo.EXPECT().
		CreateOrder(
			mock.Anything,
			mock.Anything,
		).
		Return(orderrepo.ErrOrderAlreadyProcessing)

	err := svc.UploadOrder(
		context.Background(),
		"79927398713",
		uuid.New(),
	)

	require.ErrorIs(t, err, ErrOrderAlreadyProcessing)
}

func TestService_UploadOrder_OrderAlreadyCreatedByAnotherUser(t *testing.T) {
	svc, repo, _ := newTestService(t)

	repo.EXPECT().
		CreateOrder(
			mock.Anything,
			mock.Anything,
		).
		Return(orderrepo.ErrOrderAlreadyCreatedByAnotherUser)

	err := svc.UploadOrder(
		context.Background(),
		"79927398713",
		uuid.New(),
	)

	require.ErrorIs(t, err, ErrOrderAlreadyCreatedByAnotherUser)
}

// GetOrders
func TestService_GetOrders_Success(t *testing.T) {
	svc, repo, _ := newTestService(t)

	uid := uuid.New()

	expected := []model.Order{
		{
			OrderNum: "79927398713",
			Status:   "NEW",
		},
	}

	repo.EXPECT().
		GetOrders(
			mock.Anything,
			uid,
		).
		Return(expected, nil)

	actual, err := svc.GetOrders(
		context.Background(),
		uid,
	)

	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestService_GetOrders_RepositoryError(t *testing.T) {
	svc, repo, _ := newTestService(t)

	uid := uuid.New()

	repo.EXPECT().
		GetOrders(
			mock.Anything,
			uid,
		).
		Return(nil, errors.New("database error"))

	_, err := svc.GetOrders(
		context.Background(),
		uid,
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "repository get orders")
}
