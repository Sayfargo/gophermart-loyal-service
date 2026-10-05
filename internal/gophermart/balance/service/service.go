// Package service предоставляет слой бизнес-логики для управления счетами пользователей,
// включая проверку текущего баланса, валидацию и проведение списаний, а также чтение истории выводов.
package service

import (
	"context"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/model"
	"github.com/google/uuid"

	"github.com/shopspring/decimal"
)

// BalanceRepository определяет интерфейс взаимодействия со слоем персистентного хранения данных кошельков.
type BalanceRepository interface {
	// GetBalance извлекает текущую информацию о кошельке пользователя (доступные и списанные средства).
	GetBalance(ctx context.Context, userID uuid.UUID) (model.Balance, error)
	// Withdraw выполняет транзакционное списание баллов лояльности в счет оплаты заказа.
	Withdraw(ctx context.Context, userID uuid.UUID, orderNum string, sum decimal.Decimal) error
	// GetWithdrawals возвращает хронологическую историю всех успешных списаний баллов пользователем.
	GetWithdrawals(ctx context.Context, userID uuid.UUID) ([]model.Withdrawal, error)
}

// BalanceService координирует бизнес-правила и операции, связанные с балансом и транзакциями пользователей.
type BalanceService struct {
	repo BalanceRepository
}

// New создает и инициализирует новый экземпляр BalanceService с необходимой зависимостью репозитория.
func New(balanceRepo BalanceRepository) *BalanceService {
	return &BalanceService{
		repo: balanceRepo,
	}
}

// GetBalance делегирует репозиторию запрос на получение текущего баланса и общей суммы выводов для указанного userID.
func (s *BalanceService) GetBalance(ctx context.Context, userID uuid.UUID) (model.Balance, error) {
	return s.repo.GetBalance(ctx, userID)
}

// Withdraw реализует бизнес-сценарий списания баллов лояльности:
// 1. Формирует структуру запроса и запускает сквозную валидацию (алгоритм Луна для orderNum и знак суммы).
// 2. В случае успеха передает выполнение репозиторию для атомарного проведения транзакции в БД.
func (s *BalanceService) Withdraw(
	ctx context.Context,
	userID uuid.UUID,
	orderNum string,
	sum decimal.Decimal,
) error {
	req := model.WithdrawRequest{Order: orderNum, Sum: sum}
	if err := req.Validate(); err != nil {
		return err
	}
	return s.repo.Withdraw(ctx, userID, orderNum, sum)
}

// GetWithdrawals возвращает срез всех транзакций списания для указанного userID, полученных из репозитория.
func (s *BalanceService) GetWithdrawals(ctx context.Context, userID uuid.UUID) ([]model.Withdrawal, error) {
	return s.repo.GetWithdrawals(ctx, userID)
}
