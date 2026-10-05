// Package model содержит структуры данных, перечисления и доменные модели для работы с заказами,
// товарными позициями и статусами их обработки в системе лояльности.
package model

import (
	"errors"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/luhn"
	"github.com/shopspring/decimal"
)

var (
	// ErrInvalidOrderNumber возвращается, если номер заказа не прошел проверку контрольной суммы по алгоритму Луна.
	ErrInvalidOrderNumber = errors.New("invalid order number checksum")
)

// OrderStatus определяет строковый тип для перечисления возможных жизненных циклов и статусов обработки заказа.
type OrderStatus string

const (
	// OrderStatusRegistered указывает, что заказ успешно принят системой и зарегистрирован для обработки.
	OrderStatusRegistered OrderStatus = "REGISTERED"
	// OrderStatusProcessing означает, что заказ в данный момент находится на этапе расчета баллов воркером.
	OrderStatusProcessing OrderStatus = "PROCESSING"
	// OrderStatusProcessed указывает, что расчет баллов успешно завершен и итоговая сумма начислена.
	OrderStatusProcessed OrderStatus = "PROCESSED"
	// OrderStatusInvalid означает, что заказ признан некорректным или его расчет невозможен по бизнес-правилам.
	OrderStatusInvalid OrderStatus = "INVALID"
)

// String преобразует значение статуса OrderStatus в стандартное строковое представление.
func (ost OrderStatus) String() string {
	return string(ost)
}

// Order описывает доменную модель заказа, содержащую его номер, текущий статус обработки,
// рассчитанную сумму начислений, список товаров и временную метку загрузки.
type Order struct {
	// OrderNum содержит уникальный строковый номер заказа.
	OrderNum string `json:"order"`
	// Status отражает текущий этап обработки заказа в системе.
	Status OrderStatus `json:"status,omitempty"`
	// Accrual хранит количество начисленных за заказ баллов (дробное число произвольной точности).
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
	// Goods содержит перечень товарных позиций, привязанных к данному заказу.
	Goods []Good `json:"goods,omitempty"`
	// UploadedAt фиксирует точное время регистрации заказа в системе.
	UploadedAt time.Time `json:"uploaded_at"`
}

// Good представляет доменную модель одной товарной позиции внутри заказа.
type Good struct {
	// Description содержит наименование или текстовое описание товара.
	Description string `json:"description"`
	// Price определяет стоимость одной единицы товара.
	Price decimal.Decimal `json:"price"`
}

// NewOrder создает и инициализирует новый экземпляр структуры Order с базовым статусом REGISTERED
// и текущим временем загрузки.
func NewOrder(orderNum string, goods []Good) *Order {
	return &Order{
		OrderNum:   orderNum,
		Status:     OrderStatusRegistered,
		Accrual:    nil,
		Goods:      goods,
		UploadedAt: time.Now(),
	}
}

// Validate осуществляет проверку корректности номера заказа с использованием встроенного пакета luhn.
// Возвращает ошибку ErrInvalidOrderNumber, если строка не соответствует правилам алгоритма Луна.
func (o *Order) Validate() (*Order, error) {
	if !luhn.Validate(o.OrderNum) {
		return nil, ErrInvalidOrderNumber
	}
	return o, nil
}
