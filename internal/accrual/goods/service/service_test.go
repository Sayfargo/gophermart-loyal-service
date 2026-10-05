package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
	mock "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/service/mock"
)

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func validGoods() model.GoodsInfo {
	d := mustDecimal("10")
	return model.GoodsInfo{
		Match:      "Bork",
		Reward:     &d,
		RewardType: model.RewardTypePercent,
	}
}

// TestService_RegisterGoods_Success проверяет, что валидный товар
// передаётся в репозиторий без изменений.
func TestService_RegisterGoods_Success(t *testing.T) {
	repo := mock.NewMockGoodsRepository(t)
	goodsAdder := mock.NewMockGoodsCacheAdder(t)
	repo.On("RegisterGoods", testifymock.Anything, testifymock.Anything).Return(nil)
	goodsAdder.On("Add", testifymock.Anything).Return(nil)

	svc := New(repo, goodsAdder)
	err := svc.RegisterGoods(context.Background(), validGoods())

	require.NoError(t, err)
}

// TestService_RegisterGoods_InvalidGoods проверяет, что невалидный товар
// отклоняется с ErrInvalidGoods, репозиторий не вызывается.
// Мок не настроен намеренно: вызов repo.RegisterGoods = паника, если валидация пропущена.
func TestService_RegisterGoods_InvalidGoods(t *testing.T) {
	repo := mock.NewMockGoodsRepository(t)
	goodsAdder := mock.NewMockGoodsCacheAdder(t)
	svc := New(repo, goodsAdder)

	goods := validGoods()
	goods.Match = "" // пустой match

	err := svc.RegisterGoods(context.Background(), goods)

	require.ErrorIs(t, err, model.ErrInvalidGoods)
}

// TestService_RegisterGoods_RepoError проверяет, что ошибка репозитория
// пробрасывается наверх как есть.
func TestService_RegisterGoods_RepoError(t *testing.T) {
	repo := mock.NewMockGoodsRepository(t)
	repo.On("RegisterGoods", testifymock.Anything, testifymock.Anything).
		Return(model.ErrMatchAlreadyExists)

	goodsAdder := mock.NewMockGoodsCacheAdder(t)
	svc := New(repo, goodsAdder)
	err := svc.RegisterGoods(context.Background(), validGoods())

	require.ErrorIs(t, err, model.ErrMatchAlreadyExists)
	assert.NotNil(t, err)
}
