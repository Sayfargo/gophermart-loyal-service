// Package service предоставляет слой бизнес-логики для управления учетными записями пользователей,
// включая процессы безопасной регистрации, хеширования паролей и генерации токенов аутентификации.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/model"
	userrepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/repository"
	"github.com/google/uuid"
)

var (
	// ErrUserAlreadyExists возвращается, если клиент пытается зарегистрировать уже существующий в системе логин.
	ErrUserAlreadyExists = errors.New("user already exists")
	// ErrInvalidCredentials возвращается, если при аутентификации указан неверный логин или пароль.
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// TokenBuilder определяет интерфейс для генерации строковых токенов аутентификации (JWT) на основе UUID пользователя.
type TokenBuilder interface {
	// BuildJWTString создает подписанную строку токена для указанного идентификатора пользователя.
	BuildJWTString(uuid.UUID) (string, error)
}

// PasswordHasher определяет интерфейс криптографического хеширования и безопасной проверки паролей пользователей.
type PasswordHasher interface {
	// Hash преобразует открытый пароль в безопасный криптографический хеш.
	Hash(password string) (string, error)
	// Compare выполняет константную сверку открытого пароля с сохраненным хешем.
	Compare(password, hash string) (bool, error)
}

// UserRepository определяет интерфейс взаимодействия со слоем персистентного хранения учетных записей.
type UserRepository interface {
	// FindUserInfo выполняет поиск и возврат информации о пользователе по его строковому логину.
	FindUserInfo(ctx context.Context, userLogin string) (*model.UserInfo, error)
	// SaveUserInfo транзакционно регистрирует нового пользователя и инициализирует его баланс.
	SaveUserInfo(ctx context.Context, userInfo model.UserInfo) error
}

// UserService координирует бизнес-процессы авторизации, регистрации и защиты учетных записей пользователей.
type UserService struct {
	repo           UserRepository
	tokenBuilder   TokenBuilder
	passwordHasher PasswordHasher
}

// New создает и инициализирует новый экземпляр UserService с необходимыми абстракциями криптографии и хранения.
func New(userRepo UserRepository, tokenBuilder TokenBuilder, passwordHasher PasswordHasher) *UserService {
	return &UserService{
		repo:           userRepo,
		tokenBuilder:   tokenBuilder,
		passwordHasher: passwordHasher,
	}
}

// CreateUser реализует сквозной бизнес-сценарий регистрации нового аккаунта:
// 1. Генерирует уникальный UUID пользователя (RFC 4122).
// 2. Хеширует пароль через настроенный PasswordHasher.
// 3. Формирует JWT-токен для мгновенного входа.
// 4. Транзакционно сохраняет данные в репозиторий, преобразуя ошибку уникальности в ErrUserAlreadyExists.
func (s *UserService) CreateUser(ctx context.Context, userLogin string, password string) (string, error) {
	userID, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	passwordHash, err := s.passwordHasher.Hash(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	token, err := s.tokenBuilder.BuildJWTString(userID)
	if err != nil {
		return "", fmt.Errorf("build token: %w", err)
	}
	err = s.repo.SaveUserInfo(ctx, model.UserInfo{
		UUID:         userID,
		Login:        userLogin,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		if errors.Is(err, userrepo.ErrUserAlreadyExists) {
			return "", ErrUserAlreadyExists
		}
		return "", fmt.Errorf("save user: %w", err)
	}

	return token, nil
}

// LoginUser реализует сценарий проверки подлинности пользователя (аутентификации):
// 1. Осуществляет поиск профиля по логину. Любая ошибка отсутствия записи подменяется на ErrInvalidCredentials.
// 2. Сверяет переданный пароль с сохраненным хешем в режиме защиты от атак по времени.
// 3. При успешном совпадении выпускает и возвращает новую строковую сессию JWT.
func (s *UserService) LoginUser(ctx context.Context, userLogin string, password string) (string, error) {
	userInfo, err := s.repo.FindUserInfo(ctx, userLogin)
	if err != nil {
		if errors.Is(err, userrepo.ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", fmt.Errorf("find user: %w", err)
	}

	match, err := s.passwordHasher.Compare(password, userInfo.PasswordHash)
	if err != nil {
		return "", fmt.Errorf("compare password: %w", err)
	}
	if !match {
		return "", ErrInvalidCredentials
	}

	token, err := s.tokenBuilder.BuildJWTString(userInfo.UUID)
	if err != nil {
		return "", fmt.Errorf("build token: %w", err)
	}
	return token, nil
}
