package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/accrualclient"
	ordermodel "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mocks struct {
	orders   *MockOrderStore
	balances *MockBalanceAccruer
	accrual  *MockAccrualClient
	tx       *MockTransactor
}

func newTestWorker(t *testing.T, opts ...func(*Config)) (*Worker, *mocks) {
	t.Helper()

	cfg := &Config{
		PollingInterval: time.Second,
		WorkerCount:     2,
		JobsQueueSize:   4,
		TargetRPS:       100,
	}
	for _, o := range opts {
		o(cfg)
	}

	m := &mocks{
		orders:   NewMockOrderStore(t),
		balances: NewMockBalanceAccruer(t),
		accrual:  NewMockAccrualClient(t),
		tx:       NewMockTransactor(t),
	}
	return New(m.orders, m.balances, m.accrual, m.tx, slog.New(slog.DiscardHandler), cfg), m
}

// мок ответа от accrual
func (m *mocks) accrualReplies(status accrualclient.Status, accrual *decimal.Decimal) {
	m.accrual.EXPECT().
		GetOrder(mock.Anything, "123").
		Return(&accrualclient.ResultResponse{Order: "123", Status: status, Accrual: accrual}, nil)
}

// для мока выполнения транзакции
func (m *mocks) txPassthrough() {
	m.tx.EXPECT().
		BeginFunc(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, fn func(pgx.Tx) error) error {
			return fn(nil)
		})
}

// ProcessOrder

// Проверяет поведение process order на различных кейсах (ответах от accrual)
func TestWorker_ProcessOrder_Flows(t *testing.T) {
	uid := uuid.New()
	sum := decimal.NewFromInt(500)
	errBoom := errors.New("boom")

	order := func(s ordermodel.OrderStatus) ordermodel.Order {
		return ordermodel.Order{OrderNum: "123", UserID: uid, Status: s}
	}

	tests := []struct {
		name    string
		order   ordermodel.Order
		setup   func(m *mocks)
		wantErr string
	}{
		{
			name:  "Уже PROCESSED",
			order: order(ordermodel.OrderStatusProcessed),
			setup: func(m *mocks) {},
		},
		{
			name:  "Уже INVALID",
			order: order(ordermodel.OrderStatusInvalid),
			setup: func(m *mocks) {},
		},
		{
			name:  "Accrual вернул NotRegisterd",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusNotRegistered, nil)
			},
		},
		{
			name:  "NEW -> PROCESSING",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessing, nil)
				m.orders.EXPECT().
					UpdateStatus(mock.Anything, "123", ordermodel.OrderStatusProcessing).
					Return(nil)
			},
		},
		{
			name:  "Если accrual вернул REGISTERED, то NEW -> PROCESSING",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusRegistered, nil)
				m.orders.EXPECT().
					UpdateStatus(mock.Anything, "123", ordermodel.OrderStatusProcessing).
					Return(nil)
			},
		},
		{
			name:  "Не делаем UPDATE, если уже PROCESSING",
			order: order(ordermodel.OrderStatusProcessing),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessing, nil)
			},
		},
		{
			name:  "Если PROCESSED, то статус и баланс обновляются в одной транзакции",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessed, &sum)
				m.txPassthrough()
				m.orders.EXPECT().
					UpdateOrderResultTx(mock.Anything, mock.Anything, "123", ordermodel.OrderStatusProcessed, &sum).
					Return(true, nil)
				m.balances.EXPECT().
					AccrueTx(mock.Anything, mock.Anything, uid, sum).
					Return(nil)
			},
		},
		{
			name:  "Если PROCESSED и заказ уже обновлён другим, то не трогаем",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessed, &sum)
				m.txPassthrough()
				m.orders.EXPECT().
					UpdateOrderResultTx(mock.Anything, mock.Anything, "123", ordermodel.OrderStatusProcessed, &sum).
					Return(false, nil)
			},
		},
		{
			name:  "Если PROCESSED и accrual == nil, то не баланс не обновляем",
			order: order(ordermodel.OrderStatusNew),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessed, nil)
				m.txPassthrough()
				m.orders.EXPECT().
					UpdateOrderResultTx(mock.Anything, mock.Anything, "123", ordermodel.OrderStatusProcessed, (*decimal.Decimal)(nil)).
					Return(true, nil)
			},
		},
		{
			name:  "Если INVALID обновляем только статус",
			order: order(ordermodel.OrderStatusProcessing),
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusInvalid, nil)
				m.txPassthrough()
				m.orders.EXPECT().
					UpdateOrderResultTx(mock.Anything, mock.Anything, "123", ordermodel.OrderStatusInvalid, mock.Anything).
					Return(true, nil)
			},
		},
		{
			name:    "Если ошибка в AccrueTX, то транзакция откатится",
			order:   order(ordermodel.OrderStatusNew),
			wantErr: "boom",
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.StatusProcessed, &sum)
				m.txPassthrough()
				m.orders.EXPECT().
					UpdateOrderResultTx(mock.Anything, mock.Anything, "123", ordermodel.OrderStatusProcessed, &sum).
					Return(true, nil)
				m.balances.EXPECT().
					AccrueTx(mock.Anything, mock.Anything, uid, sum).
					Return(errBoom)
			},
		},
		{
			name:    "Неизвестный статус accrual",
			order:   order(ordermodel.OrderStatusNew),
			wantErr: "unexpected accrual status",
			setup: func(m *mocks) {
				m.accrualReplies(accrualclient.Status("bigDen"), nil)
			},
		},
		{
			name:    "Ошибка при обращении к accrual",
			order:   order(ordermodel.OrderStatusNew),
			wantErr: "get order from accrual",
			setup: func(m *mocks) {
				m.accrual.EXPECT().
					GetOrder(mock.Anything, "123").
					Return(nil, errBoom)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, m := newTestWorker(t)
			tc.setup(m)

			err := w.processOrder(context.Background(), tc.order)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// Enqueue

// Проверяет, что один и тот же заказ не кидаем в джобы воркерам два раза
func TestWorker_Enqueue_DeduplicatesInflight(t *testing.T) {
	w, _ := newTestWorker(t)
	o := ordermodel.Order{OrderNum: "1"}

	assert.True(t, w.enqueue(o))
	assert.False(t, w.enqueue(o))
}

// Проверяет заполнение очереди
func TestWorker_Enqueue_QueueFull_DoesNotLeakInflight(t *testing.T) {
	w, _ := newTestWorker(t, func(c *Config) { c.JobsQueueSize = 1 })

	require.True(t, w.enqueue(ordermodel.Order{OrderNum: "1"}))
	require.False(t, w.enqueue(ordermodel.Order{OrderNum: "2"}))

	<-w.jobs
	w.inflight.Delete("1")

	assert.True(t, w.enqueue(ordermodel.Order{OrderNum: "2"}))
}

// Проверяет Notify + Polling
func TestWorker_Notify_ProcessesWithoutWaitingForTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {

		w, m := newTestWorker(t, func(c *Config) { c.PollingInterval = time.Hour })
		m.accrualReplies(accrualclient.StatusNotRegistered, nil)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); w.Run(ctx) }()

		w.Notify(ordermodel.Order{OrderNum: "123", Status: ordermodel.OrderStatusNew})
		synctest.Wait()

		m.accrual.AssertNumberOfCalls(t, "GetOrder", 1)

		cancel()
		<-done
	})
}

// Проверяет вызов GetPendingOrders по тику
func TestWorker_Run_PollingPicksPendingOrders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, m := newTestWorker(t)
		pending := ordermodel.Order{OrderNum: "123", Status: ordermodel.OrderStatusNew}

		m.orders.EXPECT().GetPendingOrders(mock.Anything).Return([]ordermodel.Order{pending}, nil).Once()
		m.orders.EXPECT().GetPendingOrders(mock.Anything).Return(nil, nil).Maybe()
		m.accrualReplies(accrualclient.StatusNotRegistered, nil)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); w.Run(ctx) }()

		time.Sleep(2*time.Second + time.Millisecond) // два тика
		synctest.Wait()

		m.accrual.AssertNumberOfCalls(t, "GetOrder", 1)

		cancel()
		<-done
	})
}

// Проверяет, что пока gate закрыт в БД не смотрим
func TestWorker_PollLoop_SkipsDBWhilePaused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, _ := newTestWorker(t)
		w.gate.PauseFor(time.Minute)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); w.pollLoop(ctx) }()

		time.Sleep(30 * time.Second)
		synctest.Wait()

		cancel()
		<-done
	})
}

// Проверяет работу ожидания после ошибки 429
func TestWorker_FetchAccrual_WaitsRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, m := newTestWorker(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m.accrual.EXPECT().GetOrder(mock.Anything, "123").
			Return(nil, &accrualclient.RateLimitError{RetryAfter: 30 * time.Second}).Once()
		m.accrual.EXPECT().GetOrder(mock.Anything, "123").
			Return(&accrualclient.ResultResponse{Order: "123", Status: accrualclient.StatusProcessing}, nil).Once()

		start := time.Now()
		res, err := w.fetchAccrual(ctx, "123")

		require.NoError(t, err)
		assert.Equal(t, accrualclient.StatusProcessing, res.Status)
		assert.GreaterOrEqual(t, time.Since(start), 30*time.Second)
	})
}

// Проверяет что если один воркер схватил 429, то остальные встанут в ожидание
func TestWorker_RateLimit_PausesAllWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, m := newTestWorker(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m.accrual.EXPECT().GetOrder(mock.Anything, mock.Anything).
			Return(nil, &accrualclient.RateLimitError{RetryAfter: time.Minute}).Once()
		m.accrual.EXPECT().GetOrder(mock.Anything, mock.Anything).
			Return(&accrualclient.ResultResponse{Status: accrualclient.StatusProcessing}, nil)

		var wg sync.WaitGroup
		fetch := func(num string) {
			wg.Go(func() {
				_, err := w.fetchAccrual(ctx, num)
				assert.NoError(t, err)
			})
		}

		fetch("1")
		synctest.Wait()
		m.accrual.AssertNumberOfCalls(t, "GetOrder", 1)

		fetch("2")
		fetch("3")
		synctest.Wait()
		m.accrual.AssertNumberOfCalls(t, "GetOrder", 1)
		time.Sleep(time.Minute)
		wg.Wait()
		m.accrual.AssertNumberOfCalls(t, "GetOrder", 4)
	})
}
