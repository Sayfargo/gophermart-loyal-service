// Package repository предоставляет функционал для взаимодействия со слоем персистентного хранения данных
// для сущностей заказов и привязанных к ним товарных позиций в СУБД PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	"github.com/shopspring/decimal"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrOrderAlreadyProcessing возвращается, если при попытке регистрации заказа обнаруживается,
	// что заказ с таким номером уже существует в базе данных.
	ErrOrderAlreadyProcessing = errors.New("order has already been uploaded")
	ErrOrderNotFound          = errors.New("order not found")
)

// OrderRepository реализует методы доступа к данным в PostgreSQL для управления заказами и товарами.
type OrderRepository struct {
	pool *pgxpool.Pool
}

// New создает и инициализирует новый экземпляр OrderRepository с использованием переданного пула соединений.
func New(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{
		pool: pool,
	}
}

// GetPendingOrders извлекает список всех заказов со статусами REGISTERED или PROCESSING,
// группируя связанные с ними товарные позиции из таблицы goods в единые доменные структуры.
// Возвращает срез незавершенных заказов, отсортированных по их номерам.
func (r *OrderRepository) GetPendingOrders(ctx context.Context) ([]model.Order, error) {
	query := `
		SELECT
    		o.order_num,
    		o.order_status,
    		g.description,
    		g.price
		FROM orders o
		JOIN goods g ON g.order_num = o.order_num
		WHERE o.order_status IN ('REGISTERED', 'PROCESSING')
		ORDER BY o.order_num
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pending orders: %w", err)
	}
	defer rows.Close()

	byOrderNum := make(map[string]*model.Order)
	var order []string

	for rows.Next() {
		var (
			orderNum, status, description string
			price                         decimal.Decimal
		)
		if err := rows.Scan(&orderNum, &status, &description, &price); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		o, ok := byOrderNum[orderNum]
		if !ok {
			o = &model.Order{OrderNum: orderNum, Status: model.OrderStatus(status)}
			byOrderNum[orderNum] = o
			order = append(order, orderNum)
		}
		o.Goods = append(o.Goods, model.Good{Description: description, Price: price})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	result := make([]model.Order, 0, len(order))
	for _, num := range order {
		result = append(result, *byOrderNum[num])
	}
	return result, nil
}

// FinalizeOrder переводит заказ в один из финальных статусов (PROCESSED или INVALID)
// и фиксирует итоговую сумму начисленных баллов. Обновление произойдет только в том случае,
// если текущий статус заказа в базе данных еще не является финальным.
// Возвращает true, если строка была успешно обновлена.
func (r *OrderRepository) FinalizeOrder(ctx context.Context, orderNum string, status model.OrderStatus, accrual decimal.Decimal) (updated bool, err error) {

	query := `
		UPDATE orders
		SET
			order_status = $1,
			accrual = $2
		WHERE
			order_num = $3
			AND order_status NOT IN ('PROCESSED', 'INVALID')
		`

	tag, err := r.pool.Exec(
		ctx,
		query,
		status,
		accrual,
		orderNum,
	)
	if err != nil {
		return false, fmt.Errorf("pool exec: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// UpdateStatus выполняет промежуточное обновление статуса заказа (например, переводит из REGISTERED в PROCESSING).
// Изменение блокируется, если заказ уже находится в терминальном состоянии (PROCESSED, INVALID).
// Возвращает true, если статус был изменен.
func (r *OrderRepository) UpdateStatus(ctx context.Context, orderNum string, status model.OrderStatus) (updated bool, err error) {

	query := `
		UPDATE orders
		SET order_status = $1
		WHERE order_num = $2
		AND order_status NOT IN ('PROCESSED', 'INVALID')
		`

	tag, err := r.pool.Exec(
		ctx,
		query,
		status,
		orderNum,
	)
	if err != nil {
		return false, fmt.Errorf("pool exec: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// GetOrder выполняет прямой SQL-запрос к базе данных для получения информации о заказе по его номеру.
// Метод считывает поля order_num, order_status и accrual.
// Возвращаемые ошибки:
//   - ErrOrderNotFound: если в таблице orders отсутствует запись с указанным order_num (маппинг pgx.ErrNoRows);
//   - Данные оборачиваются в fmt.Errorf, если произошла сетевая или синтаксическая ошибка при выполнении запроса.
func (r *OrderRepository) GetOrder(ctx context.Context, orderNum string) (model.Order, error) {

	query := `
		SELECT order_num, order_status, accrual
		FROM orders
		WHERE order_num = $1
	`

	var order model.Order

	err := r.pool.QueryRow(ctx, query, orderNum).Scan(&order.OrderNum, &order.Status, &order.Accrual)
	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return model.Order{}, ErrOrderNotFound
		}

		return model.Order{}, fmt.Errorf("query row: %w", err)
	}

	return order, nil
}

// CreateOrder атомарно (в рамках транзакции) сохраняет новый заказ в таблицу orders,
// а входящие в него товарные позиции — в таблицу goods с помощью высокопроизводительного метода CopyFrom.
// Использует хак с ON CONFLICT DO UPDATE и флагом xmax для определения скрытых коллизий при конкурентной вставке.
// Если заказ уже существовал, транзакция откатывается и возвращается ошибка ErrOrderAlreadyProcessing.
func (r *OrderRepository) CreateOrder(ctx context.Context, order *model.Order) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	query := `
		INSERT INTO orders 
			(order_num, order_status, uploaded_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (order_num) DO UPDATE SET order_num = EXCLUDED.order_num
			RETURNING (xmax != 0) AS is_conflict`

	var isConflict bool

	err = tx.QueryRow(
		ctx,
		query,
		order.OrderNum,
		order.Status,
		order.UploadedAt,
	).Scan(&isConflict)
	if err != nil {
		return fmt.Errorf("db insert: %w", err)
	}

	if isConflict {
		return ErrOrderAlreadyProcessing
	}

	var rows [][]interface{}
	for _, good := range order.Goods {
		rows = append(rows, []interface{}{good.Description, good.Price, order.OrderNum})
	}

	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"goods"},
		[]string{"description", "price", "order_num"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("db copy from: %w", err)
	}

	return tx.Commit(ctx)
}
