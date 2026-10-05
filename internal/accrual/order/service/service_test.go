package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	orderrepo "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/repository"
	"github.com/shopspring/decimal"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetOrder_Success(t *testing.T) {
	repo := NewMockOrderRepository(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := New(repo, log, nil)

	expected := model.Order{
		OrderNum: "79927398713",
		Status:   model.OrderStatusProcessed,
	}

	repo.
		EXPECT().
		GetOrder(context.Background(), "79927398713").
		Return(expected, nil).
		Once()

	got, err := svc.GetOrder(context.Background(), "79927398713")

	require.NoError(t, err)
	require.Equal(t, expected, got)

	repo.AssertExpectations(t)
}

func TestGetOrder_NotFound(t *testing.T) {
	repo := NewMockOrderRepository(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := New(repo, log, nil)

	repo.
		EXPECT().
		GetOrder(context.Background(), "79927398713").
		Return(model.Order{}, orderrepo.ErrOrderNotFound).
		Once()

	got, err := svc.GetOrder(context.Background(), "79927398713")

	require.ErrorIs(t, err, ErrOrderNotFound)
	require.Equal(t, model.Order{}, got)

	repo.AssertExpectations(t)
}

func TestGetOrder_RepositoryError(t *testing.T) {
	repo := NewMockOrderRepository(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := New(repo, log, nil)

	repoErr := errors.New("database unavailable")

	repo.
		EXPECT().
		GetOrder(context.Background(), "79927398713").
		Return(model.Order{}, repoErr).
		Once()

	got, err := svc.GetOrder(context.Background(), "79927398713")

	require.Error(t, err)
	require.ErrorContains(t, err, "repo get order")
	require.ErrorIs(t, err, repoErr)
	require.Equal(t, model.Order{}, got)

	repo.AssertExpectations(t)
}

func TestUploadOrder_Success(t *testing.T) {
	repo := NewMockOrderRepository(t)
	notifier := NewMockOrderNotifier(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := New(repo, log, notifier)
	svc.notifier = notifier

	order := model.Order{
		OrderNum: "79927398713",
		Goods: []model.Good{
			{
				Description: "Tea",
				Price:       decimal.RequireFromString("100"),
			},
		},
	}

	repo.
		EXPECT().CreateOrder(context.Background(), mock.AnythingOfType("*model.Order")).
		Return(nil).
		Once()

	notifier.
		EXPECT().Notify(mock.AnythingOfType("model.Order")).
		Once()

	err := svc.UploadOrder(context.Background(), order)

	require.NoError(t, err)

	repo.AssertExpectations(t)
	notifier.AssertExpectations(t)
}

func TestUploadOrder_InvalidOrderNumber(t *testing.T) {
	repo := NewMockOrderRepository(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := New(repo, log, nil)

	order := model.Order{
		OrderNum: "12345",
	}

	err := svc.UploadOrder(context.Background(), order)

	require.ErrorIs(t, err, ErrInvalidOrderNum)
}
