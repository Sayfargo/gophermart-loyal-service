package goodscache

import (
	"context"
	"errors"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	rules []model.GoodsInfo
	err   error
}

func (m *mockProvider) GetAllGoods(context.Context) ([]model.GoodsInfo, error) {
	return m.rules, m.err
}

func TestNew(t *testing.T) {
	provider := &mockProvider{}

	cache := New(provider)

	require.NotNil(t, cache)
	assert.Empty(t, cache.Get())
	assert.Equal(t, provider, cache.provider)
}

func TestGoodsCache_SetGet(t *testing.T) {
	cache := New(nil)

	d := decimal.RequireFromString("10")

	rules := []model.GoodsInfo{
		{
			Match:      "Bork",
			Reward:     &d,
			RewardType: model.RewardTypePercent,
		},
	}

	cache.Set(rules)

	assert.Equal(t, rules, cache.Get())
}

func TestGoodsCache_Add(t *testing.T) {
	cache := New(nil)

	d := decimal.RequireFromString("10")

	rule := model.GoodsInfo{
		Match:      "Bork",
		Reward:     &d,
		RewardType: model.RewardTypePercent,
	}

	cache.Add(rule)

	got := cache.Get()

	require.Len(t, got, 1)
	assert.Equal(t, rule, got[0])
}

func TestGoodsCache_Load(t *testing.T) {
	d := decimal.RequireFromString("10")

	rules := []model.GoodsInfo{
		{
			Match:      "Bork",
			Reward:     &d,
			RewardType: model.RewardTypePercent,
		},
	}

	cache := New(&mockProvider{
		rules: rules,
	})

	err := cache.Load(context.Background())

	require.NoError(t, err)
	assert.Equal(t, rules, cache.Get())
}

func TestGoodsCache_Load_Error(t *testing.T) {
	cache := New(&mockProvider{
		err: errors.New("db error"),
	})

	err := cache.Load(context.Background())

	require.Error(t, err)
	assert.Empty(t, cache.Get())
}
