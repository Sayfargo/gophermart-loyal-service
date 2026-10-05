// Package repository предоставляет функционал для взаимодействия со слоем постоянного хранения данных (хранилищем)
// для сущностей и правил, связанных с товарами и вознаграждениями в СУБД PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GoodsRepository реализует методы доступа к данным в PostgreSQL для управления
// правилами начисления баллов за товары.
type GoodsRepository struct {
	pool *pgxpool.Pool
}

// New создает и инициализирует новый экземпляр GoodsRepository с использованием переданного пула соединений.
func New(pool *pgxpool.Pool) *GoodsRepository {
	return &GoodsRepository{
		pool: pool,
	}
}

// RegisterGoods сохраняет новое правило начисления вознаграждения в базу данных.
// Если правило для указанного шаблона (match) уже существует (нарушение ограничения уникальности 23505),
// метод перехватывает ошибку PostgreSQL и возвращает доменную ошибку model.ErrMatchAlreadyExists.
func (r *GoodsRepository) RegisterGoods(ctx context.Context, goods model.GoodsInfo) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO rewards (match, reward_value, reward_type) VALUES ($1, $2, $3)`,
		goods.Match, goods.Reward, goods.RewardType,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrMatchAlreadyExists
		}
	}
	return err
}

// GetAllGoods извлекает все зарегистрированные правила начисления вознаграждений из базы данных.
// Возвращает срез структур model.GoodsInfo. Метод гарантирует закрытие строк результата (rows)
// и проверяет наличие ошибок итерации через rows.Err().
func (r *GoodsRepository) GetAllGoods(ctx context.Context) ([]model.GoodsInfo, error) {
	rows, err := r.pool.Query(ctx, `SELECT match, reward_value, reward_type FROM rewards`)
	if err != nil {
		return nil, fmt.Errorf("pool query: %w", err)
	}
	defer rows.Close()

	var goods []model.GoodsInfo
	for rows.Next() {
		var g model.GoodsInfo
		if err := rows.Scan(&g.Match, &g.Reward, &g.RewardType); err != nil {
			return nil, fmt.Errorf("rows scan: %w", err)
		}
		goods = append(goods, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return goods, nil
}
