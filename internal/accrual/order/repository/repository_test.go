package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/testenv"
	accrualMigrations "github.com/Sayfargo/gophermart-loyal-service/migrations/accrual"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	env := testenv.Setup(ctx, accrualMigrations.EmbedMigrations)
	testPool = env.Pool

	code := m.Run()
	env.Close(ctx)
	os.Exit(code)
}

func cleanDB(t *testing.T) {
	t.Helper()

	_, err := testPool.Exec(context.Background(), `TRUNCATE goods, orders RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func newRepo(t *testing.T) *OrderRepository {
	t.Helper()
	return New(testPool)
}

func insertOrder(t *testing.T, orderNum string, status model.OrderStatus, goods ...model.Good) {
	t.Helper()
	ctx := context.Background()

	_, err := testPool.Exec(ctx,
		`INSERT INTO orders (order_num, order_status, uploaded_at) VALUES ($1, $2, $3)`,
		orderNum, status, time.Now(),
	)
	require.NoError(t, err)

	for _, g := range goods {
		_, err := testPool.Exec(ctx,
			`INSERT INTO goods (description, price, order_num) VALUES ($1, $2, $3)`,
			g.Description, g.Price, orderNum,
		)
		require.NoError(t, err)
	}
}

func fetchOrderStatusAndAccrual(t *testing.T, orderNum string) (model.OrderStatus, *decimal.Decimal) {
	t.Helper()

	var (
		status  model.OrderStatus
		accrual *decimal.Decimal
	)
	err := testPool.QueryRow(context.Background(),
		`SELECT order_status, accrual FROM orders WHERE order_num = $1`,
		orderNum,
	).Scan(&status, &accrual)
	require.NoError(t, err)

	return status, accrual
}

// Createorder, GetPendingOrders, FinalizeOrder, UpdateStatus, GetOrder

func TestOrderRepository_GetOrder_Success(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	insertOrder(t, "123", model.OrderStatusProcessed)

	accrual := decimal.RequireFromString("123.45")
	_, err := testPool.Exec(
		context.Background(),
		`UPDATE orders SET accrual = $1 WHERE order_num = $2`,
		accrual,
		"123",
	)
	require.NoError(t, err)

	order, err := repo.GetOrder(context.Background(), "123")

	require.NoError(t, err)

	assert.Equal(t, "123", order.OrderNum)
	assert.Equal(t, model.OrderStatusProcessed, order.Status)

	require.NotNil(t, order.Accrual)
	assert.True(t, order.Accrual.Equal(accrual))
}

func TestOrderRepository_GetOrder_NotFound(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	order, err := repo.GetOrder(context.Background(), "does-not-exist")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOrderNotFound)
	assert.Equal(t, model.Order{}, order)
}

func TestOrderRepository_CreateOrder_Success(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	order := &model.Order{
		OrderNum: "123",
		Status:   model.OrderStatusRegistered,
		Goods: []model.Good{
			{Description: "Чайник Bork", Price: decimal.NewFromInt(7000)},
			{Description: "Утюг Bork", Price: decimal.NewFromInt(3000)},
		},
	}

	err := repo.CreateOrder(context.Background(), order)
	require.NoError(t, err)

	status, accrual := fetchOrderStatusAndAccrual(t, "123")
	assert.Equal(t, model.OrderStatusRegistered, status)
	assert.Nil(t, accrual, "accrual ещё не должен быть рассчитан")

	var goodsCount int
	err = testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM goods WHERE order_num = $1`, "123",
	).Scan(&goodsCount)
	require.NoError(t, err)
	assert.Equal(t, 2, goodsCount)
}

func TestOrderRepository_CreateOrder_EmptyGoods(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	order := &model.Order{OrderNum: "456", Status: model.OrderStatusRegistered}

	err := repo.CreateOrder(context.Background(), order)
	require.NoError(t, err)

	var goodsCount int
	err = testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM goods WHERE order_num = $1`, "456",
	).Scan(&goodsCount)
	require.NoError(t, err)
	assert.Equal(t, 0, goodsCount)
}

func TestOrderRepository_CreateOrder_Conflict(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	ctx := context.Background()

	order := &model.Order{
		OrderNum: "789",
		Status:   model.OrderStatusRegistered,
		Goods:    []model.Good{{Description: "Чайник Bork", Price: decimal.NewFromInt(7000)}},
	}
	require.NoError(t, repo.CreateOrder(ctx, order))

	// повторная регистрация того же номера заказа
	err := repo.CreateOrder(ctx, &model.Order{
		OrderNum: "789",
		Status:   model.OrderStatusRegistered,
		Goods:    []model.Good{{Description: "Другой товар", Price: decimal.NewFromInt(100)}},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOrderAlreadyProcessing))

	var goodsCount int
	err = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM goods WHERE order_num = $1`, "789").Scan(&goodsCount)
	require.NoError(t, err)
	assert.Equal(t, 1, goodsCount, "конфликтующая вставка не должна была добавить товары")
}

func TestOrderRepository_GetPendingOrders_ReturnsOnlyActiveStatuses(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	insertOrder(t, "1", model.OrderStatusRegistered,
		model.Good{Description: "Чайник Bork", Price: decimal.NewFromInt(7000)},
		model.Good{Description: "Утюг Bork", Price: decimal.NewFromInt(3000)},
	)
	insertOrder(t, "2", model.OrderStatusProcessing,
		model.Good{Description: "Тостер Tefal", Price: decimal.NewFromInt(2000)},
	)
	insertOrder(t, "3", model.OrderStatusProcessed,
		model.Good{Description: "Утюг Bork", Price: decimal.NewFromInt(3000)},
	)
	insertOrder(t, "4", model.OrderStatusInvalid)

	orders, err := repo.GetPendingOrders(context.Background())
	require.NoError(t, err)
	require.Len(t, orders, 2, "PROCESSED и INVALID не должны попасть в выборку")

	byNum := make(map[string]model.Order)
	for _, o := range orders {
		byNum[o.OrderNum] = o
	}

	require.Contains(t, byNum, "1")
	require.Contains(t, byNum, "2")

	assert.Equal(t, model.OrderStatusRegistered, byNum["1"].Status)
	assert.Len(t, byNum["1"].Goods, 2, "товары заказа 1 должны сгруппироваться вместе")

	assert.Equal(t, model.OrderStatusProcessing, byNum["2"].Status)
	require.Len(t, byNum["2"].Goods, 1)
	assert.Equal(t, "Тостер Tefal", byNum["2"].Goods[0].Description)
}

func TestOrderRepository_GetPendingOrders_NoRows(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	orders, err := repo.GetPendingOrders(context.Background())

	require.NoError(t, err)
	assert.Empty(t, orders)
}

func TestOrderRepository_FinalizeOrder_UpdatesWhenNotFinalized(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	insertOrder(t, "1", model.OrderStatusProcessing,
		model.Good{Description: "Чайник Bork", Price: decimal.NewFromInt(7000)},
	)

	accrual := decimal.NewFromFloat(700.5)
	updated, err := repo.FinalizeOrder(context.Background(), "1", model.OrderStatusProcessed, accrual)

	require.NoError(t, err)
	assert.True(t, updated)

	status, gotAccrual := fetchOrderStatusAndAccrual(t, "1")
	assert.Equal(t, model.OrderStatusProcessed, status)
	require.NotNil(t, gotAccrual)
	assert.True(t, gotAccrual.Equal(accrual))
}

func TestOrderRepository_FinalizeOrder_NoopWhenAlreadyProcessed(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	insertOrder(t, "1", model.OrderStatusProcessing)

	original := decimal.NewFromInt(500)
	updated, err := repo.FinalizeOrder(context.Background(), "1", model.OrderStatusProcessed, original)
	require.NoError(t, err)
	require.True(t, updated)

	// повторная финализация с другим значением — не должна пройти
	again := decimal.NewFromInt(999)
	updated, err = repo.FinalizeOrder(context.Background(), "1", model.OrderStatusProcessed, again)

	require.NoError(t, err)
	assert.False(t, updated, "заказ уже PROCESSED — повторное обновление должно быть отклонено")

	_, gotAccrual := fetchOrderStatusAndAccrual(t, "1")
	require.NotNil(t, gotAccrual)
	assert.True(t, gotAccrual.Equal(original), "значение не должно было перезаписаться")
}

func TestOrderRepository_FinalizeOrder_NoopWhenInvalid(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	insertOrder(t, "1", model.OrderStatusInvalid)

	updated, err := repo.FinalizeOrder(context.Background(), "1", model.OrderStatusProcessed, decimal.NewFromInt(100))

	require.NoError(t, err)
	assert.False(t, updated)
}

func TestOrderRepository_FinalizeOrder_UnknownOrder(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)

	updated, err := repo.FinalizeOrder(context.Background(), "does-not-exist", model.OrderStatusProcessed, decimal.NewFromInt(1))

	require.NoError(t, err)
	assert.False(t, updated)
}

func TestOrderRepository_UpdateStatus_UpdatesWhenNotFinalized(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	insertOrder(t, "1", model.OrderStatusRegistered)

	updated, err := repo.UpdateStatus(context.Background(), "1", model.OrderStatusProcessing)

	require.NoError(t, err)
	assert.True(t, updated)

	status, _ := fetchOrderStatusAndAccrual(t, "1")
	assert.Equal(t, model.OrderStatusProcessing, status)
}

func TestOrderRepository_UpdateStatus_NoopWhenAlreadyFinalized(t *testing.T) {
	cleanDB(t)
	repo := newRepo(t)
	insertOrder(t, "1", model.OrderStatusProcessed)

	updated, err := repo.UpdateStatus(context.Background(), "1", model.OrderStatusProcessing)

	require.NoError(t, err)
	assert.False(t, updated, "финализированный заказ нельзя вернуть в PROCESSING")

	status, _ := fetchOrderStatusAndAccrual(t, "1")
	assert.Equal(t, model.OrderStatusProcessed, status, "статус не должен был измениться")
}
