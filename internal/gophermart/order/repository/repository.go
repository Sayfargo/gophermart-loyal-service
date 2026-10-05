// Package repository предоставляет функционал для взаимодействия со слоем персистентного хранения данных
// для сущностей заказов пользователей в СУБД PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// OrderRepository реализует методы доступа к PostgreSQL для управления заказами пользователей.
type OrderRepository struct {
	pool *pgxpool.Pool
}

var (
	// ErrOrderAlreadyCreatedByAnotherUser возвращается, если при попытке регистрации заказа обнаруживается,
	// что этот номер заказа уже был загружен другим пользователем системы.
	ErrOrderAlreadyCreatedByAnotherUser = errors.New("order already created by another user")
	// ErrOrderAlreadyProcessing возвращается, если текущий пользователь пытается повторно загрузить
	// свой собственный заказ, который уже находится в обработке или обработан.
	ErrOrderAlreadyProcessing = errors.New("order has already been uploaded by this user")
)

// New создает и инициализирует новый экземпляр OrderRepository с использованием переданного пула соединений.
func New(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{
		pool: pool,
	}
}

// UpdateOrderResultTx обновляет статус заказа и итоговую сумму начисления (accrual) в рамках внешней открытой транзакции pgx.Tx.
// Операция выполняется по принципу CAS (Compare-And-Swap) и блокируется, если заказ уже находится в терминальном состоянии (PROCESSED, INVALID).
// Возвращает true, если строка была успешно обновлена.
func (r *OrderRepository) UpdateOrderResultTx(
	ctx context.Context,
	tx pgx.Tx, orderNum string,
	status model.OrderStatus,
	accrual *decimal.Decimal,
) (bool, error) {

	query := `
		UPDATE orders
		SET order_status = $1, accrual = $2
		WHERE order_num = $3
		AND order_status NOT IN ('PROCESSED', 'INVALID')
	`

	result, err := tx.Exec(
		ctx,
		query,
		status.String(),
		accrual,
		orderNum,
	)

	if err != nil {
		return false, fmt.Errorf("tx exec: %w", err)
	}

	return result.RowsAffected() > 0, nil
}

// GetPendingOrders возвращает список всех недозавершенных заказов со статусами NEW или PROCESSING
// для их последующей отправки на сверку во внешнюю систему расчетов.
func (r *OrderRepository) GetPendingOrders(ctx context.Context) ([]model.Order, error) {

	query := `
		SELECT order_num, user_id, order_status, accrual, uploaded_at
		FROM orders
		WHERE order_status IN ('NEW', 'PROCESSING') 
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("pool query: %w", err)
	}
	defer rows.Close()

	orders := make([]model.Order, 0, 32)

	for rows.Next() {
		var o model.Order

		if err := rows.Scan(
			&o.OrderNum,
			&o.UserID,
			&o.Status,
			&o.Accrual,
			&o.UploadedAt,
		); err != nil {
			return nil, fmt.Errorf("rows scan: %w", err)
		}

		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return orders, nil
}

// UpdateStatus выполняет безусловное обновление статуса конкретного заказа по его номеру.
// Возвращает ошибку, если в базе данных не найдено записи с указанным номером.
func (r *OrderRepository) UpdateStatus(ctx context.Context, orderNum string, status model.OrderStatus) error {

	query := `
		UPDATE orders SET order_status = $1
		WHERE order_num = $2
	`

	result, err := r.pool.Exec(ctx, query, status.String(), orderNum)
	if err != nil {
		return fmt.Errorf("pool exec: %w", err)
	}

	if result.RowsAffected() == 0 {
		return errors.New("there is no order with that num")
	}

	return nil

}

// GetOrders извлекает полный перечень всех заказов, загруженных конкретным пользователем (uid).
// Результаты сортируются по дате загрузки в хронологическом порядке (от старых к новым).
func (r *OrderRepository) GetOrders(ctx context.Context, uid uuid.UUID) ([]model.Order, error) {

	query := `
		SELECT order_num, order_status, accrual, uploaded_at
		FROM orders
		WHERE user_id = $1
		ORDER BY uploaded_at ASC
	`

	orders := make([]model.Order, 0, 32)

	rows, err := r.pool.Query(ctx, query, uid)
	if err != nil {
		return nil, fmt.Errorf("pool query: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var o model.Order

		if err := rows.Scan(
			&o.OrderNum,
			&o.Status,
			&o.Accrual,
			&o.UploadedAt,
		); err != nil {
			return nil, fmt.Errorf("rows scan: %w", err)
		}

		orders = append(orders, o)

	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return orders, nil

}

// CreateOrder пытается зарегистрировать новый заказ в системе.
// Использует атомарную вставку с разрешением конфликтов через upsert-механизм (ON CONFLICT DO UPDATE) и внутреннюю системную переменную xmax.
// Позволяет бесконфликтно определить, кем именно и когда был создан данный заказ:
//   - Если заказ уже существует и заведен другим пользователем, возвращает ErrOrderAlreadyCreatedByAnotherUser.
//   - Если заказ повторно загружен текущим владельцем, возвращает ErrOrderAlreadyProcessing.
func (r *OrderRepository) CreateOrder(ctx context.Context, order *model.Order) error {

	query := `
		INSERT INTO orders 
			(order_num, user_id, order_status, accrual, uploaded_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (order_num) DO UPDATE SET order_num = EXCLUDED.order_num
			RETURNING user_id, (xmax != 0) AS is_conflict`

	var (
		existingUserID uuid.UUID
		isConflict     bool
	)

	err := r.pool.QueryRow(
		ctx,
		query,
		order.OrderNum,
		order.UserID,
		order.Status,
		order.Accrual,
		order.UploadedAt,
	).Scan(&existingUserID, &isConflict)

	if err != nil {
		return fmt.Errorf("db insert: %w", err)
	}

	if isConflict {
		if existingUserID != order.UserID {
			return ErrOrderAlreadyCreatedByAnotherUser
		}
		return ErrOrderAlreadyProcessing
	}

	return nil
}
