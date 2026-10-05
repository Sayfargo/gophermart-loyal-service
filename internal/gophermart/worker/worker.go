// Package worker предоставляет асинхронный воркер-пул для синхронизации состояний заказов
// с внешней системой расчетов (accrual) и транзакционного начисления баллов на баланс пользователей.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/accrualclient"
	ordermodel "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// OrderStore определяет интерфейс взаимодействия с базой данных для управления жизненным циклом заказов.
type OrderStore interface {
	// GetPendingOrders - возвращает заказы со статусами NEW или PROCESSING
	GetPendingOrders(ctx context.Context) ([]ordermodel.Order, error)
	// UpdateOrderResult — атомарно обновляет статус+accrual, только если заказ ещё не обработан
	UpdateOrderResultTx(ctx context.Context, tx pgx.Tx, orderNum string, status ordermodel.OrderStatus, accrual *decimal.Decimal) (updated bool, err error)
	// UpdateStatus - обовляет только статус заказа
	UpdateStatus(ctx context.Context, orderNum string, status ordermodel.OrderStatus) error
}

// BalanceAccruer - интерфейс для взаимодействия с балансом пользователя
type BalanceAccruer interface {
	// Accrue - обновляет баланс пользователя используяю транзакцию
	AccrueTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, sum decimal.Decimal) error
}

// AccrualClient - интерфейс для взаимодействия с внешним сервисом через клиент
type AccrualClient interface {
	// GetOrder - возвращает результат с содержанием текущего состояния заказа из accrual
	GetOrder(ctx context.Context, orderNum string) (*accrualclient.ResultResponse, error)
}

// Transactor предоставляет абстракцию для управления границами транзакций базы данных через функцию обратного вызова.
type Transactor interface {
	// BeginFunc выполняет переданную функцию fn внутри изолированной ACID-транзакции СУБД.
	BeginFunc(ctx context.Context, fn func(pgx.Tx) error) error
}

// Worker координирует конкурентный конвейер обработки заказов (Worker Pool) и механизм поллинга БД.
// Включает внутренний механизм защиты (gate) для динамической приостановки запросов при получении ошибок HTTP 429.
type Worker struct {
	orders   OrderStore
	balances BalanceAccruer
	accrual  AccrualClient
	tx       Transactor
	logger   *slog.Logger

	interval time.Duration
	workers  int
	jobs     chan ordermodel.Order // канал джобсов для воркеров
	inflight sync.Map              // потокобезопасный syns.Map для содержания очередей джобсов

	gate *gate // закрываем запросы к accrual если схватили 429 (too many requests)
}

// New создает, конфигурирует и возвращает полностью инициализированный экземпляр Worker для сервиса gophermart.
func New(
	orders OrderStore,
	balances BalanceAccruer,
	accrual AccrualClient,
	tx Transactor,
	logger *slog.Logger,
	c *Config,
) *Worker {
	return &Worker{
		orders: orders, balances: balances, accrual: accrual, tx: tx,
		logger: logger, interval: c.PollingInterval, workers: c.WorkerCount,
		jobs: make(chan ordermodel.Order, c.JobsQueueSize),

		gate: newGate(c.TargetRPS, logger),
	}
}

// enqueue — кладёт джобы в очереди, не блокируая работу
// Возвращает false если джоба уже в очереди или канал джобов полон
func (w *Worker) enqueue(order ordermodel.Order) bool {
	// гарантирует что не будет гонки с заказами - два одинаковых одновременно попасть в обработку не смогут
	if _, loaded := w.inflight.LoadOrStore(order.OrderNum, struct{}{}); loaded {
		return false
	}
	select {
	case w.jobs <- order:
		return true
	default:
		// Елси канал джобов заполнен, мы удаляем его из очереди и возвращаем false
		// чтобы в будущем мы ошибчно его не скипнули
		w.inflight.Delete(order.OrderNum)
		return false
	}
}

// Notify отправляет заказ на обработку в очередь воркеров.
// Если заказ уже обрабатывается в другом потоке или буфер переполнен, операция безопасно
// логируется и пропускается, так как заказ гарантированно подхватит поллинг-воркер базы данных.
func (w *Worker) Notify(order ordermodel.Order) {
	if !w.enqueue(order) {
		w.logger.Debug(
			"not enqueued, polling will pick it up",
			"order_num", order.OrderNum,
		)
	}
}

// Run - запускает работу воркера
// Воркер либо принимает order из канала (notify) либо по тикеру бежит в БД и ищет NEW или PROCESSING заказы
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	// Запускает воркеров
	for range w.workers {
		wg.Go(func() {
			w.workerLoop(ctx)
		})
	}
	// Одноичный polling воркер который иногда бегает в БД
	wg.Go(func() {
		w.pollLoop(ctx)
	})
	// Гарантия того, что по gracefull shutdown дождёмся всех воркеров
	wg.Wait()
}

func (w *Worker) workerLoop(ctx context.Context) {
	for {
		// Либо хэндлит ордер либо выходит по контексту
		select {
		case <-ctx.Done():
			return
		case order := <-w.jobs:
			w.handle(ctx, order)
		}
	}
}

func (w *Worker) handle(ctx context.Context, order ordermodel.Order) {
	// перед выходом освобождает ордер из очереди
	defer w.inflight.Delete(order.OrderNum)
	if err := w.processOrder(ctx, order); err != nil {
		w.logger.Error(
			"process order failed", "order_num",
			order.OrderNum, "err", err,
		)
	}
}

func (w *Worker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// По тикеру бегает в БД и достаёт ордера с NEW или PROCESSING статусами
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Если словили 429, то нет смысла ходить в БД
			if w.gate.Remaining() > 0 {
				continue
			}
			orders, err := w.orders.GetPendingOrders(ctx)
			if err != nil {
				w.logger.Error(
					"fetch pending orders failed",
					"err", err,
				)
				continue
			}
			for _, o := range orders {
				w.enqueue(o)
			}
		}
	}
}

func (w *Worker) fetchAccrual(ctx context.Context, orderNum string) (*accrualclient.ResultResponse, error) {
	for {
		// Если словили 429, то остальные воркеры застынут в ожидание на этой строчке
		if err := w.gate.Wait(ctx); err != nil {
			return nil, err
		}

		// даже если в это горлышко протиснутся другие воркеры, мы не сделаем x2 x3 к паузе
		// так как в PauseFor мы отталкиваемся от текущего времени и прибавим максимум копеечные
		// миллисекунды

		res, err := w.accrual.GetOrder(ctx, orderNum)

		var rl *accrualclient.RateLimitError
		if errors.As(err, &rl) {
			w.gate.PauseFor(rl.RetryAfter)
			w.logger.Warn(
				"accrual rate limited, pausing requests",
				"retry_after", rl.RetryAfter,
			)
			continue // после паузы отправляем воркера обратно к точке Wait
		}
		return res, err
	}
}

// processOrder — единственная точка входа для всех заказов, из Notifier и из Polling в том числе
// если заказы попали в processOrder одновременно, это решается на уровне repository в UpdateOrderResult
func (w *Worker) processOrder(ctx context.Context, order ordermodel.Order) error {
	// Если статус заказа уже PROCESSED или INVALID (попала старая версия заказа), то мы просто промолчим ничего не меняю
	if order.Status == ordermodel.OrderStatusProcessed || order.Status == ordermodel.OrderStatusInvalid {
		return nil
	}

	result, err := w.fetchAccrual(ctx, order.OrderNum)
	if err != nil {
		return fmt.Errorf("get order from accrual: %w", err)
	}

	switch result.Status {
	case accrualclient.StatusNotRegistered:
		return nil
	case accrualclient.StatusRegistered, accrualclient.StatusProcessing:
		if order.Status != ordermodel.OrderStatusProcessing {
			return w.orders.UpdateStatus(ctx, order.OrderNum, ordermodel.OrderStatusProcessing)
		}
		return nil
	case accrualclient.StatusProcessed, accrualclient.StatusInvalid:
		return w.finalizeOrder(ctx, order, result)
	default:
		return fmt.Errorf("unexpected accrual status: %s", result.Status)
	}
}

// finalizeOrder - переводит заказ в конечный статус и начисляет боннусы, если статус не INVALID
func (w *Worker) finalizeOrder(ctx context.Context, order ordermodel.Order, result *accrualclient.ResultResponse) error {
	status := toInternalStatus(result.Status)

	return w.tx.BeginFunc(ctx, func(tx pgx.Tx) error {
		updated, err := w.orders.UpdateOrderResultTx(ctx, tx, order.OrderNum, status, result.Accrual)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}

		if status == ordermodel.OrderStatusProcessed && result.Accrual != nil {
			return w.balances.AccrueTx(ctx, tx, order.UserID, *result.Accrual)
		}
		return nil
	})
}

// toInternalStatus - хелпер для конвертации статуса accrual в статус, который понимает gophermart
func toInternalStatus(accrualStatus accrualclient.Status) ordermodel.OrderStatus {
	switch accrualStatus {
	case accrualclient.StatusProcessed:
		return ordermodel.OrderStatusProcessed
	case accrualclient.StatusInvalid:
		return ordermodel.OrderStatusInvalid
	case accrualclient.StatusProcessing, accrualclient.StatusRegistered:
		return ordermodel.OrderStatusProcessing
	default:
		return ordermodel.OrderStatusNew
	}
}
