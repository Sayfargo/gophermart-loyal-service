// Package worker предоставляет асинхронный конвейер (pipeline) для фоновой обработки
// и калькуляции баллов лояльности по зарегистрированным заказам.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	goodsmodel "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
	ordermodel "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	"github.com/shopspring/decimal"
)

// OrderStore определяет интерфейс взаимодействия с базой данных для управления жизненным циклом заказов.
// Все методы изменения состояния должны гарантировать атомарность операций (CAS - Compare-And-Swap).
type OrderStore interface {
	// UpdateStatus атомарно переводит заказ в новый статус (например, PROCESSING),
	// только если текущий статус еще не является терминальным.
	// Возвращает updated=false, если состояние изменилось параллельным процессом.
	UpdateStatus(ctx context.Context, orderNum string, status ordermodel.OrderStatus) (updated bool, err error)
	// FinalizeOrder атомарно фиксирует финальный статус (PROCESSED/INVALID) и итоговую сумму начислений accrual.
	FinalizeOrder(ctx context.Context, orderNum string, status ordermodel.OrderStatus, accrual decimal.Decimal) (updated bool, err error)
	// GetPendingOrders возвращает список заказов, ожидающих вычисления баллов.
	GetPendingOrders(ctx context.Context) ([]ordermodel.Order, error)
}

// GoodsCacheGetter - интерфейс для получения правил начисления из кэша
type GoodsCacheGetter interface {
	// Get - возвращает все правила начисления из кэша
	Get() []goodsmodel.GoodsInfo
}

// Worker инкапсулирует конкурентную очередь задач (Worker Pool) и механизм периодического опроса БД (Polling),
// обеспечивая надежную обработку и начисление баллов без дублирования операций над заказами.
type Worker struct {
	orders OrderStore
	logger *slog.Logger

	interval time.Duration
	workers  int
	jobs     chan ordermodel.Order
	inflight sync.Map

	goodsCache GoodsCacheGetter
}

// New создает, настраивает и возвращает экземпляр Worker с заданным пулом воркеров и размером очереди задач.
func New(
	orders OrderStore,
	goodsCache GoodsCacheGetter,
	logger *slog.Logger,
	c *Config,
) *Worker {
	return &Worker{
		orders: orders, logger: logger, goodsCache: goodsCache,
		interval: c.PollingInterval,
		workers:  c.WorkerCount,
		jobs:     make(chan ordermodel.Order, c.JobsQueueSize),
	}
}

// Run - запускает работу воркера
// Воркер либо принимает order из канала (notify) либо по тикеру бежит в БД и ищет REGISTERED или PROCESSING заказы
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

	// По тикеру бегает в БД и достаёт ордера с REGISTERED или PROCESSING статусами
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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

// Notify отправляет сообщение воркеру, принимая order в качестве параметра
func (w *Worker) Notify(order ordermodel.Order) {
	if !w.enqueue(order) {
		w.logger.Debug(
			"not enqueued, polling will pick it up",
			"order_num", order.OrderNum,
		)
	}
}

// processOrder - обрабатывает ордер, переводя его в PROCESSING и считая начисление
func (w *Worker) processOrder(ctx context.Context, order ordermodel.Order) error {
	if order.Status == ordermodel.OrderStatusProcessed || order.Status == ordermodel.OrderStatusInvalid {
		return nil
	}

	if len(order.Goods) == 0 {
		// если пустой состав заказа, то нечего считать, финализируем сразу как INVALID
		_, err := w.orders.FinalizeOrder(ctx, order.OrderNum, ordermodel.OrderStatusInvalid, decimal.Zero)
		return err
	}

	if order.Status != ordermodel.OrderStatusProcessing {
		updated, err := w.orders.UpdateStatus(ctx, order.OrderNum, ordermodel.OrderStatusProcessing)
		if err != nil {
			return fmt.Errorf("update status to processing: %w", err)
		}
		if !updated {
			return nil
		}
	}

	total := w.calculateAccrual(order.Goods)

	_, err := w.orders.FinalizeOrder(ctx, order.OrderNum, ordermodel.OrderStatusProcessed, total)
	if err != nil {
		return fmt.Errorf("finalize order: %w", err)
	}
	return nil
}

// calculateAccrual - считает начисление по правилам из кэша
func (w *Worker) calculateAccrual(goods []ordermodel.Good) decimal.Decimal {
	rules := w.goodsCache.Get()

	total := decimal.Zero
	for _, item := range goods {
		for _, rule := range rules {
			// Сравниваем описание товара с правилом начисления, игнорируя регистр
			if !strings.Contains(strings.ToLower(item.Description), strings.ToLower(rule.Match)) {
				continue
			}
			total = total.Add(rewardFor(rule, item.Price))
		}
	}
	return total
}

func rewardFor(rewards goodsmodel.GoodsInfo, price decimal.Decimal) decimal.Decimal {
	switch rewards.RewardType {
	case goodsmodel.RewardTypePercent:
		// Вычисляем процент от цены и делим на 100, чтобы получить правильное начисление
		return price.Mul(*rewards.Reward).Div(decimal.NewFromInt(100))
	case goodsmodel.RewardTypePoints:
		// Вычисляем начисление в виде очков
		return *rewards.Reward
	default:
		return decimal.Zero
	}
}
