package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"

	goodsmodel "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
	ordermodel "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Проверяет, что воркер корректно обрабатывает заказ и начисляет баллы
func TestWorker_ProcessOrder_Success(t *testing.T) {
	ctx := context.Background()

	orderStore := NewMockOrderStore(t)
	cache := NewMockGoodsCacheGetter(t)

	reward := decimal.RequireFromString("10")

	cache.EXPECT().
		Get().
		Return([]goodsmodel.GoodsInfo{
			{
				Match:      "apple",
				Reward:     &reward,
				RewardType: goodsmodel.RewardTypePercent,
			},
		})

	orderStore.EXPECT().
		UpdateStatus(
			mock.Anything,
			"1",
			ordermodel.OrderStatusProcessing,
		).
		Return(true, nil)

	expected := decimal.RequireFromString("100")

	orderStore.EXPECT().
		FinalizeOrder(
			mock.Anything,
			"1",
			ordermodel.OrderStatusProcessed,
			mock.MatchedBy(func(d decimal.Decimal) bool {
				return d.Equal(expected)
			}),
		).
		Return(true, nil)

	w := &Worker{
		orders:     orderStore,
		goodsCache: cache,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := w.processOrder(ctx, ordermodel.Order{
		OrderNum: "1",
		Status:   ordermodel.OrderStatusRegistered,
		Goods: []ordermodel.Good{
			{
				Description: "Apple Watch",
				Price:       decimal.RequireFromString("1000"),
			},
		},
	})

	require.NoError(t, err)
}

// Проверяет, что воркер корректно обрабатывает заказ, который уже в статусе PROCESSING
func TestWorker_ProcessOrder_AlreadyProcessing(t *testing.T) {
	ctx := context.Background()

	orderStore := NewMockOrderStore(t)
	cache := NewMockGoodsCacheGetter(t)

	reward := decimal.RequireFromString("10")

	cache.EXPECT().
		Get().
		Return([]goodsmodel.GoodsInfo{
			{
				Match:      "apple",
				Reward:     &reward,
				RewardType: goodsmodel.RewardTypePercent,
			},
		})

	expected := decimal.RequireFromString("100")

	orderStore.EXPECT().
		FinalizeOrder(
			mock.Anything,
			"1",
			ordermodel.OrderStatusProcessed,
			mock.MatchedBy(func(d decimal.Decimal) bool {
				return d.Equal(expected)
			}),
		).
		Return(true, nil)

	w := &Worker{
		orders:     orderStore,
		goodsCache: cache,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := w.processOrder(ctx, ordermodel.Order{
		OrderNum: "1",
		Status:   ordermodel.OrderStatusProcessing,
		Goods: []ordermodel.Good{
			{
				Description: "Apple Watch",
				Price:       decimal.RequireFromString("1000"),
			},
		},
	})

	require.NoError(t, err)
}

// Проверяет, что воркер корректно обрабатывает заказ с пустым составом товаров и переводит его в статус INVALID
func TestWorker_ProcessOrder_EmptyGoods(t *testing.T) {
	ctx := context.Background()

	orderStore := NewMockOrderStore(t)

	orderStore.EXPECT().
		FinalizeOrder(
			mock.Anything,
			"1",
			ordermodel.OrderStatusInvalid,
			decimal.Zero,
		).
		Return(true, nil)

	w := &Worker{
		orders: orderStore,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := w.processOrder(ctx, ordermodel.Order{
		OrderNum: "1",
	})

	require.NoError(t, err)
}

// Проверяет, что воркер корректно обрабатывает заказ и пропускает обновление статуса, если оно уже было обновлено другим процессом
func TestWorker_ProcessOrder_UpdateStatusSkipped(t *testing.T) {
	ctx := context.Background()

	orderStore := NewMockOrderStore(t)

	orderStore.EXPECT().
		UpdateStatus(
			mock.Anything,
			"1",
			ordermodel.OrderStatusProcessing,
		).
		Return(false, nil)

	w := &Worker{
		orders: orderStore,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := w.processOrder(ctx, ordermodel.Order{
		OrderNum: "1",
		Status:   ordermodel.OrderStatusRegistered,
		Goods: []ordermodel.Good{
			{
				Description: "Apple",
				Price:       decimal.RequireFromString("100"),
			},
		},
	})

	require.NoError(t, err)
}

// CalculateAccrual - тесты для функции calculateAccrual, которая вычисляет сумму начислений по заказу на основе правил из кэша товаров
func TestWorker_CalculateAccrual(t *testing.T) {
	reward := decimal.RequireFromString("10")

	cache := NewMockGoodsCacheGetter(t)

	cache.EXPECT().
		Get().
		Return([]goodsmodel.GoodsInfo{
			{
				Match:      "apple",
				Reward:     &reward,
				RewardType: goodsmodel.RewardTypePercent,
			},
		})

	w := &Worker{
		goodsCache: cache,
	}

	total := w.calculateAccrual([]ordermodel.Good{
		{
			Description: "Apple Watch",
			Price:       decimal.RequireFromString("1000"),
		},
	})

	assert.True(t, decimal.RequireFromString("100").Equal(total))
}

// rewardFor - тесты для функции rewardFor, которая вычисляет начисление по правилу и цене товара
func TestRewardFor_Percent(t *testing.T) {
	reward := decimal.RequireFromString("10")

	got := rewardFor(
		goodsmodel.GoodsInfo{
			Reward:     &reward,
			RewardType: goodsmodel.RewardTypePercent,
		},
		decimal.RequireFromString("500"),
	)

	assert.True(t, decimal.RequireFromString("50").Equal(got))
}

func TestRewardFor_Points(t *testing.T) {
	reward := decimal.RequireFromString("250")

	got := rewardFor(
		goodsmodel.GoodsInfo{
			Reward:     &reward,
			RewardType: goodsmodel.RewardTypePoints,
		},
		decimal.RequireFromString("500"),
	)

	assert.True(t, decimal.RequireFromString("250").Equal(got))
}

func TestRewardFor_UnknownType(t *testing.T) {
	reward := decimal.RequireFromString("10")

	got := rewardFor(
		goodsmodel.GoodsInfo{
			Reward:     &reward,
			RewardType: "unknown",
		},
		decimal.RequireFromString("500"),
	)

	assert.True(t, decimal.Zero.Equal(got))
}
