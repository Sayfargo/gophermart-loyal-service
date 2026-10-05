package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/balance/model"
)

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// TestService_GetBalance_Success проверяет, что сервис возвращает баланс,
// полученный от репозитория, без изменений.
func TestService_GetBalance_Success(t *testing.T) {
	userID := uuid.New()
	expected := model.Balance{
		UserID:    userID,
		Current:   mustDecimal("500.5"),
		Withdrawn: mustDecimal("42"),
	}

	repo := NewMockBalanceRepository(t)
	repo.On("GetBalance", context.Background(), userID).Return(expected, nil)

	svc := New(repo)
	got, err := svc.GetBalance(context.Background(), userID)

	require.NoError(t, err)
	assert.True(t, expected.Current.Equal(got.Current), "current mismatch")
	assert.True(t, expected.Withdrawn.Equal(got.Withdrawn), "withdrawn mismatch")
	repo.AssertExpectations(t)
}

// TestService_GetBalance_RepoError проверяет, что ошибка репозитория
// пробрасывается наверх без оборачивания.
func TestService_GetBalance_RepoError(t *testing.T) {
	userID := uuid.New()
	repoErr := errors.New("db is down")

	repo := NewMockBalanceRepository(t)
	repo.On("GetBalance", context.Background(), userID).
		Return(model.Balance{}, repoErr)

	svc := New(repo)
	_, err := svc.GetBalance(context.Background(), userID)

	require.ErrorIs(t, err, repoErr)
	repo.AssertExpectations(t)
}

// TestService_Withdraw_Success проверяет успешный сценарий:
// валидный номер и сумма, репозиторий вызван с теми же аргументами.
func TestService_Withdraw_Success(t *testing.T) {
	userID := uuid.New()
	repo := NewMockBalanceRepository(t)
	repo.On("Withdraw", context.Background(), userID, "12345678903", mustDecimal("100")).
		Return(nil)

	svc := New(repo)
	err := svc.Withdraw(context.Background(), userID, "12345678903", mustDecimal("100"))

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// TestService_Withdraw_InvalidLuhn проверяет, что номер, не прошедший Луна,
// отклоняется с ErrInvalidOrderNumber, репозиторий не вызывается.
func TestService_Withdraw_InvalidLuhn(t *testing.T) {
	repo := NewMockBalanceRepository(t)
	svc := New(repo)

	err := svc.Withdraw(context.Background(), uuid.New(), "12345678904", mustDecimal("100"))

	require.ErrorIs(t, err, model.ErrInvalidOrderNumber)

}

// TestService_Withdraw_ZeroSum проверяет, что нулевая сумма отклоняется
// с ErrInvalidOrderNumber до обращения к репозиторию.
func TestService_Withdraw_ZeroSum(t *testing.T) {
	repo := NewMockBalanceRepository(t)
	svc := New(repo)

	err := svc.Withdraw(context.Background(), uuid.New(), "12345678903", decimal.Zero)

	require.ErrorIs(t, err, model.ErrInvalidOrderNumber)
}

// TestService_Withdraw_InsufficientFunds проверяет, что ошибка
// ErrInsufficientFunds от репозитория пробрасывается наверх как есть.
func TestService_Withdraw_InsufficientFunds(t *testing.T) {
	userID := uuid.New()
	repo := NewMockBalanceRepository(t)
	repo.On("Withdraw", context.Background(), userID, "12345678903", mustDecimal("100")).
		Return(model.ErrInsufficientFunds)

	svc := New(repo)
	err := svc.Withdraw(context.Background(), userID, "12345678903", mustDecimal("100"))

	require.ErrorIs(t, err, model.ErrInsufficientFunds)
	repo.AssertExpectations(t)
}

func TestService_GetWithdrawals_Success(t *testing.T) {
	userID := uuid.New()
	expected := []model.Withdrawal{{Order: "12345678903", Sum: mustDecimal("100")}}

	repo := NewMockBalanceRepository(t)
	repo.On("GetWithdrawals", context.Background(), userID).Return(expected, nil)

	svc := New(repo)
	got, err := svc.GetWithdrawals(context.Background(), userID)

	require.NoError(t, err)
	assert.Len(t, got, 1)
	repo.AssertExpectations(t)
}

func TestService_GetWithdrawals_RepoError(t *testing.T) {
	userID := uuid.New()
	repoErr := errors.New("db is down")

	repo := NewMockBalanceRepository(t)
	repo.On("GetWithdrawals", context.Background(), userID).Return(nil, repoErr)

	svc := New(repo)
	_, err := svc.GetWithdrawals(context.Background(), userID)

	require.ErrorIs(t, err, repoErr)
	repo.AssertExpectations(t)
}
