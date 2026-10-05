// Package repository предоставляет функционал для взаимодействия со слоем персистентного хранения данных
// для управления учетными записями пользователей, их аутентификационными данными и кошельками в СУБД PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrUserAlreadyExists возвращается, если при попытке регистрации пользователя обнаруживается,
	// что логин (уникальный ключ) уже занят другим аккаунтом.
	ErrUserAlreadyExists = errors.New("user already exists")
	// ErrUserNotFound возвращается, если в процессе аутентификации или поиска пользователь с указанным логином не найден.
	ErrUserNotFound = errors.New("user not found")
)

// UserRepository реализует методы доступа к PostgreSQL для управления данными пользователей.
type UserRepository struct {
	pool *pgxpool.Pool
}

// New создает и инициализирует новый экземпляр UserRepository с использованием переданного пула соединений.
func New(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{
		pool: pool,
	}
}

// FindUserInfo выполняет поиск и извлечение полной информации о пользователе по его уникальному строковому логину.
// Если запись отсутствует в таблице users, метод перехватывает ошибку pgx.ErrNoRows и возвращает доменную ошибку ErrUserNotFound.
func (r *UserRepository) FindUserInfo(ctx context.Context, userLogin string) (*model.UserInfo, error) {
	userInfo := &model.UserInfo{}
	query := `
		SELECT id, login, password_hash, created_at
		FROM users WHERE login = $1
	`
	err := r.pool.QueryRow(ctx, query, userLogin).Scan(&userInfo.UUID, &userInfo.Login, &userInfo.PasswordHash, &userInfo.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return userInfo, nil
}

// SaveUserInfo атомарно (в рамках транзакции) регистрирует нового пользователя в системе:
// 1. Создает запись в таблице users. В случае коллизии по уникальному логину (код ошибки 23505), возвращает ErrUserAlreadyExists.
// 2. Инициализирует пустой стартовый кошелек с нулевым балансом в таблице balance, связанный с UUID нового пользователя.
func (r *UserRepository) SaveUserInfo(ctx context.Context, userInfo model.UserInfo) error {

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	query := `
		INSERT INTO users(id, login, password_hash, created_at)
		VALUES ($1,$2, $3, $4)
	`
	_, err = tx.Exec(
		ctx,
		query,
		userInfo.UUID,
		userInfo.Login,
		userInfo.PasswordHash,
		userInfo.CreatedAt,
	)

	var pgErr *pgconn.PgError
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("insert user: %w", err)
	}

	queryBalance := `INSERT INTO balance (user_uuid, current, withdrawn) VALUES ($1, 0, 0)`

	_, err = tx.Exec(ctx, queryBalance, userInfo.UUID)
	if err != nil {
		return fmt.Errorf("insert balance: %w", err)
	}

	return tx.Commit(ctx)
}
