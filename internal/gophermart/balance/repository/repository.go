// Package repository предоставляет функционал для взаимодействия со слоем персистентного хранения данных
// для управления балансами пользователей и историей списания баллов в СУБД PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// BalanceRepository реализует методы доступа к PostgreSQL для ведения счетов пользователей.
type BalanceRepository struct {
	pool *pgxpool.Pool
}

// New создает и инициализирует новый экземпляр BalanceRepository с использованием переданного пула соединений.
func New(pool *pgxpool.Pool) *BalanceRepository {
	return &BalanceRepository{pool: pool}
}

// GetBalance возвращает текущую информацию о доступном балансе и сумме выводов пользователя.
// Если запись о счете пользователя отсутствует (pgx.ErrNoRows), метод не возвращает ошибку,
// а инициализирует и отдает пустую доменную структуру model.Balance с нулевыми балансами.
func (r *BalanceRepository) GetBalance(ctx context.Context, userID uuid.UUID) (model.Balance, error) {
	const query = `
    SELECT user_uuid, current, withdrawn
    FROM balance
    WHERE user_uuid = $1
`

	var b model.Balance
	err := r.pool.QueryRow(ctx, query, userID).Scan(&b.UserID, &b.Current, &b.Withdrawn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Balance{UserID: userID}, nil
		}
		return model.Balance{}, fmt.Errorf("query balance: %w", err)
	}
	return b, nil
}

// AccrueTx выполняет операцию зачисления баллов на счет пользователя в рамках внешней открытой транзакции pgx.Tx.
// Возвращает ошибку, если запись кошелька пользователя не найдена (RowsAffected == 0).
func (r *BalanceRepository) AccrueTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	sum decimal.Decimal,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE balance SET current = current + $1 WHERE user_uuid = $2`,
		sum, userID,
	)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("balance row not found for user %s", userID)
	}

	return nil
}

// Withdraw атомарно в рамках транзакции выполняет списание средств со счета пользователя:
// 1. Блокирует строку баланса для обновления (`FOR UPDATE`) и проверяет наличие достаточного количества средств.
// 2. Уменьшает доступный баланс и увеличивает счетчик списаний.
// 3. Фиксирует операцию в таблице истории транзакций transactions.
// Если номер заказа уже фигурировал в истории (уникальный индекс, ошибка 23505), возвращает ошибку model.ErrOrderAlreadyUsed.
func (r *BalanceRepository) Withdraw(
	ctx context.Context,
	userID uuid.UUID,
	orderNum string,
	sum decimal.Decimal,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Извлечение текущего баланса
	var current decimal.Decimal
	err = tx.QueryRow(ctx,
		`SELECT current FROM balance WHERE user_uuid = $1 FOR UPDATE`,
		userID,
	).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrInsufficientFunds
		}
		return fmt.Errorf("select balance for update: %w", err)
	}
	if current.LessThan(sum) {
		return model.ErrInsufficientFunds
	}
	// Списание
	_, err = tx.Exec(ctx,
		`UPDATE balance
			SET current = current - $1,
				withdrawn = withdrawn + $1
			WHERE user_uuid = $2`,
		sum, userID,
	)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}

	// Добавление операции в историю
	_, err = tx.Exec(ctx,
		`INSERT INTO transactions (user_uuid, order_num, sum) VALUES ($1, $2, $3)`,
		userID, orderNum, sum,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrOrderAlreadyUsed
		}
		return fmt.Errorf("insert transaction: %w", err)
	}

	return tx.Commit(ctx)

}

// GetWithdrawals возвращает хронологический список всех успешных операций списания баллов
// конкретного пользователя, отсортированных по дате проведения от старых к новым.
func (r *BalanceRepository) GetWithdrawals(ctx context.Context, userID uuid.UUID) ([]model.Withdrawal, error) {
	const query = `
    SELECT order_num, sum, processed_at
    FROM transactions
	WHERE user_uuid = $1
    ORDER BY processed_at ASC
`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query withdrawals: %w", err)

	}
	defer rows.Close()
	var result []model.Withdrawal
	for rows.Next() {
		var w model.Withdrawal
		if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, fmt.Errorf("scan withdrawal: %w", err)
		}
		result = append(result, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate withdrawals: %w", err)
	}
	return result, nil
}
