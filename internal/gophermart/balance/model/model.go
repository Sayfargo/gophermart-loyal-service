// Package model содержит структуры данных, доменные модели и логику валидации
// для подсистемы ведения баланса и учета списания баллов лояльности.
package model

import (
	"errors"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/luhn"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	// Номер заказа не прошёл валидацию Луна.
	ErrInvalidOrderNumber = errors.New("invalid order number")

	// На балансе недостаточно средств.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// Заказ уже использовался для списания.
	ErrOrderAlreadyUsed = errors.New("order already used")
)

// Balance представляет доменную модель текущего финансового состояния счета пользователя,
// включая доступные средства и общую сумму ранее списанных баллов.
type Balance struct {
	// UserID содержит уникальный криптографический идентификатор владельца счета.
	UserID uuid.UUID `json:"-"`
	// Current определяет текущее количество доступных для списания баллов лояльности.
	Current decimal.Decimal `json:"current"`
	// Withdrawn фиксирует агрегированную сумму всех успешных списаний за всё время существования счета.
	Withdrawn decimal.Decimal `json:"withdrawn"`
}

// WithdrawRequest описывает структуру тела входящего HTTP-запроса (JSON),
// отправляемого пользователем для списания баллов в счет оплаты нового заказа.
type WithdrawRequest struct {
	// Order содержит уникальный строковый номер заказа, на который оформляется списание.
	Order string `json:"order"`
	// Sum определяет точное количество баллов лояльности, запрашиваемых к выводу.
	Sum decimal.Decimal `json:"sum"`
}

// Validate выполняет доменную валидацию полей запроса на списание средств.
// Проверяет номер заказа по алгоритму Луна и контролирует, что запрашиваемая сумма строго положительна.
// Возвращает ошибку ErrInvalidOrderNumber в случае несоответствия бизнес-правилам.
func (r WithdrawRequest) Validate() error {
	if !luhn.Validate(r.Order) {
		return ErrInvalidOrderNumber
	}
	if !r.Sum.IsPositive() {
		return ErrInvalidOrderNumber
	}
	return nil
}

// Withdrawal отражает историческую доменную модель одной успешной транзакции
// по списанию баллов лояльности со счета пользователя.
type Withdrawal struct {
	// Order содержит строковый номер заказа, в рамках которого производилась оплата баллами.
	Order string `json:"order"`
	// Sum определяет количество списанных баллов по данной операции.
	Sum decimal.Decimal `json:"sum"`
	// ProcessedAt фиксирует точное время успешного проведения транзакции списания.
	ProcessedAt time.Time `json:"processed_at"`
}
