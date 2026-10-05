// Package model содержит структуры данных, перечисления статусов и доменные модели
// для управления сущностями заказов пользователей в основном сервисе gophermart.
package model

import (
	"errors"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/luhn"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	// ErrInvalidOrderNumber возвращается, если номер заказа не прошел валидацию контрольной суммы по алгоритму Луна.
	ErrInvalidOrderNumber = errors.New("invalid order number checksum")
)

// OrderStatus определяет строковый тип для представления текущего статуса обработки заказа в системе gophermart.
type OrderStatus string

const (
	// OrderStatusNew указывает, что заказ новый и только что загружен пользователем в систему.
	OrderStatusNew OrderStatus = "NEW"
	// OrderStatusProcessing означает, что заказ принят к обработке и синхронизируется с внешней системой начислений.
	OrderStatusProcessing OrderStatus = "PROCESSING"
	// OrderStatusProcessed указывает на успешное завершение расчета и финальное начисление баллов.
	OrderStatusProcessed OrderStatus = "PROCESSED"
	// OrderStatusInvalid означает, что заказ признан некорректным внешней системой расчетов.
	OrderStatusInvalid OrderStatus = "INVALID"
)

// String возвращает строковое представление статуса заказа OrderStatus.
func (ost OrderStatus) String() string {
	return string(ost)
}

// Order описывает базовую доменную модель заказа пользователя, фиксируя его уникальный номер,
// статус обработки, рассчитанную сумму начислений и временную метку загрузки.
type Order struct {
	// OrderNum содержит уникальный строковый номер заказа.
	OrderNum string
	// UserID хранит UUID пользователя, зарегистрировавшего данный заказ.
	UserID uuid.UUID
	// Status отражает текущий этап обработки заказа.
	Status OrderStatus
	// Accrual содержит количество начисленных баллов лояльности (присутствует только при успешном расчете).
	Accrual *decimal.Decimal
	// UploadedAt фиксирует точную дату и время добавления заказа в систему.
	UploadedAt time.Time
}

// NewOrder создает и возвращает новый указатель на структуру Order с базовым статусом NEW
// и текущим временем загрузки.
func NewOrder(orderNum string, uid uuid.UUID) *Order {
	return &Order{
		OrderNum:   orderNum,
		UserID:     uid,
		Status:     OrderStatusNew,
		Accrual:    nil,
		UploadedAt: time.Now(),
	}
}

// Validate производит проверку корректности номера заказа с помощью встроенного пакета luhn.
// Возвращает ошибку ErrInvalidOrderNumber, если строка не соответствует контрольной сумме алгоритма Луна.
func (o *Order) Validate() (*Order, error) {
	if !luhn.Validate(o.OrderNum) {
		return nil, ErrInvalidOrderNumber
	}
	return o, nil
}
