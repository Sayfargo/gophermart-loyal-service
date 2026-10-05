package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/testenv"
	gophermartMigrations "github.com/Sayfargo/gophermart-loyal-service/migrations/gophermart"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	env := testenv.Setup(ctx, gophermartMigrations.EmbedMigrations)
	testPool = env.Pool

	code := m.Run()
	env.Close(ctx)
	os.Exit(code)
}

func cleanDB(t *testing.T) {
	t.Helper()

	_, err := testPool.Exec(context.Background(), `TRUNCATE users, orders RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func newRepo(t *testing.T) *OrderRepository {
	t.Helper()

	return New(testPool)
}

func TestOrderRepository_UpdateStatus_OrderNotFound(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := New(testPool)

	err := repo.UpdateStatus(
		ctx,
		"does-not-exist",
		model.OrderStatusProcessing,
	)

	require.Error(t, err)
	assert.EqualError(t, err, "there is no order with that num")
}

func TestOrderRepository_UpdateStatus(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := New(testPool)

	userID := uuid.New()

	queryUser := `
		INSERT INTO users 
		(id, login, password_hash) 
		VALUES ($1, $2, $3)
	`
	_, err := testPool.Exec(ctx, queryUser, userID, "test_login", "test_password_hash")
	require.NoError(t, err)

	_, err = testPool.Exec(ctx, `
		INSERT INTO orders(order_num, user_id, order_status)
		VALUES ($1,$2,$3)
	`, "12345678903", userID, model.OrderStatusNew.String())

	require.NoError(t, err)

	err = repo.UpdateStatus(
		ctx,
		"12345678903",
		model.OrderStatusProcessing,
	)

	require.NoError(t, err)

	var status string

	err = testPool.QueryRow(
		ctx,
		`SELECT order_status FROM orders WHERE order_num=$1`,
		"12345678903",
	).Scan(&status)

	require.NoError(t, err)
	assert.Equal(t, model.OrderStatusProcessing.String(), status)
}

func TestOrderRepository_GetPendingOrders(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := New(testPool)

	userID := uuid.New()

	queryUser := `
		INSERT INTO users 
		(id, login, password_hash) 
		VALUES ($1, $2, $3)
	`
	_, err := testPool.Exec(ctx, queryUser, userID, "test_login", "test_password_hash")
	require.NoError(t, err)

	_, err = testPool.Exec(ctx, `
		INSERT INTO orders(order_num, user_id, order_status)
		VALUES
		('1',$1,'NEW'),
		('2',$1,'PROCESSING'),
		('3',$1,'PROCESSED'),
		('4',$1,'INVALID')
	`, userID)

	require.NoError(t, err)

	orders, err := repo.GetPendingOrders(ctx)

	require.NoError(t, err)
	require.Len(t, orders, 2)

	statuses := map[string]model.OrderStatus{}

	for _, order := range orders {
		statuses[order.OrderNum] = order.Status
	}

	assert.Equal(t, model.OrderStatusNew, statuses["1"])
	assert.Equal(t, model.OrderStatusProcessing, statuses["2"])
}

func TestOrderRepository_UpdateOrderResultTx_AlreadyProcessed(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := New(testPool)

	userID := uuid.New()

	queryUser := `
		INSERT INTO users 
		(id, login, password_hash) 
		VALUES ($1, $2, $3)
	`
	_, err := testPool.Exec(ctx, queryUser, userID, "test_login", "test_password_hash")
	require.NoError(t, err)

	queryOrder := `
		INSERT INTO orders
		(order_num, user_id, order_status)
		VALUES ($1, $2, $3)
	`
	_, err = testPool.Exec(ctx, queryOrder, "12345678903", userID, model.OrderStatusProcessed.String())
	require.NoError(t, err)

	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)

	accrual := decimal.NewFromInt(100)

	updated, err := repo.UpdateOrderResultTx(
		ctx,
		tx,
		"12345678903",
		model.OrderStatusProcessed,
		&accrual,
	)

	require.NoError(t, err)
	assert.False(t, updated)

	require.NoError(t, tx.Commit(ctx))
}

func TestOrderRepository_UpdateOrderResultTx(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := New(testPool)

	userID := uuid.New()

	queryUser := `
		INSERT INTO users 
		(id, login, password_hash) 
		VALUES ($1, $2, $3)
	`
	_, err := testPool.Exec(ctx, queryUser, userID, "test_login", "test_password_hash")
	require.NoError(t, err)

	queryOrder := `
		INSERT INTO orders
		(order_num, user_id, order_status)
		VALUES ($1, $2, $3)
	`
	_, err = testPool.Exec(ctx, queryOrder, "12345678903", userID, model.OrderStatusNew.String())
	require.NoError(t, err)

	accrual := decimal.NewFromFloat(500.25)

	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)

	updated, err := repo.UpdateOrderResultTx(
		ctx, tx, "12345678903", model.OrderStatusProcessed, &accrual,
	)
	require.NoError(t, err)
	require.True(t, updated)

	require.NoError(t, tx.Commit(ctx))

	var (
		status string
		value  decimal.Decimal
	)
	err = testPool.QueryRow(
		ctx,
		`SELECT order_status, accrual FROM orders WHERE order_num=$1`,
		"12345678903",
	).Scan(&status, &value)

	require.NoError(t, err)
	assert.Equal(t, model.OrderStatusProcessed.String(), status)
	assert.True(t, value.Equal(accrual))
}

// CreateOrder
func TestCreateOrder_Success(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := newRepo(t)

	var (
		expectedOrderNum   = "12345678903"
		expectedUserID     = uuid.New()
		expectedStatus     = model.OrderStatusNew
		expectedUploadedAt = time.Now().UTC()
	)

	userQuery := `INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)`
	_, err := testPool.Exec(ctx, userQuery, expectedUserID, "test_login", "test_password_hash")
	require.NoError(t, err)

	var order = &model.Order{
		OrderNum:   expectedOrderNum,
		UserID:     expectedUserID,
		Status:     expectedStatus,
		UploadedAt: expectedUploadedAt,
	}

	err = repo.CreateOrder(ctx, order)
	require.NoError(t, err)

	query := `
		SELECT order_num, user_id, order_status, accrual, uploaded_at
			FROM orders WHERE order_num = $1
		`

	var result model.Order

	err = testPool.QueryRow(
		ctx,
		query,
		expectedOrderNum,
	).Scan(
		&result.OrderNum,
		&result.UserID,
		&result.Status,
		&result.Accrual,
		&result.UploadedAt,
	)
	require.NoError(t, err)

	require.Nil(t, result.Accrual)

	assert.Equal(t, expectedOrderNum, result.OrderNum)
	assert.Equal(t, expectedUserID, result.UserID)
	assert.Equal(t, expectedStatus, result.Status)
	require.WithinDuration(t, expectedUploadedAt, result.UploadedAt, time.Millisecond)
}

func TestCreateOrder_OrderAlreadyProcessing(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()
	repo := newRepo(t)

	userID := uuid.New()

	_, err := testPool.Exec(
		ctx,
		`INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)`,
		userID,
		"test_login",
		"test_password_hash",
	)
	require.NoError(t, err)

	order := &model.Order{
		OrderNum:   "12345678903",
		UserID:     userID,
		Status:     model.OrderStatusNew,
		UploadedAt: time.Now().UTC(),
	}

	require.NoError(t, repo.CreateOrder(ctx, order))

	err = repo.CreateOrder(ctx, order)

	require.ErrorIs(t, err, ErrOrderAlreadyProcessing)
}

func TestCreateOrder_OrderAlreadyCreatedByAnotherUser(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()
	repo := newRepo(t)

	users := []struct {
		UserID uuid.UUID
		Login  string
	}{
		{uuid.New(), "user1"},
		{uuid.New(), "user2"},
	}

	for _, user := range users {
		_, err := testPool.Exec(
			ctx,
			`INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)`,
			user.UserID,
			user.Login,
			"hash",
		)
		require.NoError(t, err)
	}

	firstOrder := &model.Order{
		OrderNum:   "12345678903",
		UserID:     users[0].UserID,
		Status:     model.OrderStatusNew,
		UploadedAt: time.Now().UTC(),
	}

	require.NoError(t, repo.CreateOrder(ctx, firstOrder))

	secondOrder := &model.Order{
		OrderNum:   "12345678903",
		UserID:     users[1].UserID,
		Status:     model.OrderStatusNew,
		UploadedAt: time.Now().UTC(),
	}

	err := repo.CreateOrder(ctx, secondOrder)

	require.ErrorIs(t, err, ErrOrderAlreadyCreatedByAnotherUser)
}

// GetOrders
func TestGetOrders_Success(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()
	repo := newRepo(t)

	userID := uuid.New()
	anotherUserID := uuid.New()

	users := []struct {
		id    uuid.UUID
		login string
	}{
		{userID, "user1"},
		{anotherUserID, "user2"},
	}

	for _, u := range users {
		_, err := testPool.Exec(
			ctx,
			`INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)`,
			u.id,
			u.login,
			"hash",
		)
		require.NoError(t, err)
	}

	oldTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	newTime := time.Now().UTC().Truncate(time.Millisecond)

	_, err := testPool.Exec(ctx, `
		INSERT INTO orders(order_num, user_id, order_status, accrual, uploaded_at)
		VALUES
			($1,$2,$3,$4,$5),
			($6,$7,$8,$9,$10),
			($11,$12,$13,$14,$15)
	`,
		"12345678903", userID, model.OrderStatusNew, nil, oldTime,
		"79927398713", userID, model.OrderStatusProcessed, nil, newTime,
		"11111111111", anotherUserID, model.OrderStatusNew, nil, oldTime,
	)
	require.NoError(t, err)

	expected := []model.Order{
		{
			OrderNum:   "12345678903",
			Status:     model.OrderStatusNew,
			UploadedAt: oldTime,
		},
		{
			OrderNum:   "79927398713",
			Status:     model.OrderStatusProcessed,
			UploadedAt: newTime,
		},
	}

	orders, err := repo.GetOrders(ctx, userID)

	require.NoError(t, err)
	require.Len(t, orders, 2)

	assert.Equal(t, expected[0].OrderNum, orders[0].OrderNum)
	assert.Equal(t, expected[0].Status, orders[0].Status)
	assert.True(t, expected[0].UploadedAt.Equal(orders[0].UploadedAt))

	assert.Equal(t, expected[1].OrderNum, orders[1].OrderNum)
	assert.Equal(t, expected[1].Status, orders[1].Status)
	assert.True(t, expected[1].UploadedAt.Equal(orders[1].UploadedAt))
}

func TestGetOrders_Empty(t *testing.T) {
	cleanDB(t)

	ctx := context.Background()
	repo := newRepo(t)

	userID := uuid.New()

	_, err := testPool.Exec(
		ctx,
		`INSERT INTO users(id, login, password_hash) VALUES ($1,$2,$3)`,
		userID,
		"test_login",
		"test_hash",
	)
	require.NoError(t, err)

	orders, err := repo.GetOrders(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, orders)
}
