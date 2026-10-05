// Package service предоставляет слой бизнес-логики для управления заказами пользователей в gophermart,
// включая обработку загрузки новых заказов, их сквозную валидацию, персистентное сохранение и асинхронное уведомление.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/model"
	orderrepo "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/order/repository"
	"github.com/google/uuid"
)

// OrderNotifier определяет интерфейс асинхронного оповещения фоновых систем (воркеров)
// о регистрации нового заказа для последующего обновления его статуса во внешней системе начислений.
type OrderNotifier interface {
	// Notify отправляет заказ на конвейер обработки и актуализации баланса.
	Notify(order model.Order)
}

// OrderRepository определяет интерфейс взаимодействия со слоем постоянного хранения данных заказов.
type OrderRepository interface {
	// CreateOrder персистентно сохраняет новый заказ в базу данных.
	CreateOrder(ctx context.Context, order *model.Order) error
	// GetOrders извлекает полную историю заказов, загруженных конкретным пользователем.
	GetOrders(ctx context.Context, uid uuid.UUID) ([]model.Order, error)
}

// OrderService инкапсулирует репозиторий, логгер и систему нотификации
// для обеспечения выполнения бизнес-сценариев жизненного цикла заказов.
type OrderService struct {
	repo     OrderRepository
	l        *slog.Logger
	notifier OrderNotifier
}

var (
	// ErrInvalidOrderNum возвращается, если строка номера заказа не прошла алгоритм валидации Луна.
	ErrInvalidOrderNum = errors.New("invalid order number")
	// ErrOrderAlreadyProcessing возвращается, если данный заказ уже был успешно загружен текущим пользователем.
	ErrOrderAlreadyProcessing = errors.New("order has already been uploaded by this user")
	// ErrOrderAlreadyCreatedByAnotherUser возвращается, если указанный номер заказа уже заведен в системе другим клиентом.
	ErrOrderAlreadyCreatedByAnotherUser = errors.New("order already created by another user")
	// ErrOrdersNotFound возвращается при запросе истории, если у пользователя еще нет ни одного оформленного заказа.
	ErrOrdersNotFound = errors.New("orders not found")
)

// New создает и инициализирует новый экземпляр OrderService с необходимыми внешними зависимостями.
func New(
	repo OrderRepository,
	log *slog.Logger,
	notifier OrderNotifier,
) *OrderService {
	return &OrderService{
		repo:     repo,
		l:        log,
		notifier: notifier,
	}
}

// GetOrders возвращает список всех заказов, принадлежащих пользователю с идентификатором uid.
func (s *OrderService) GetOrders(ctx context.Context, uid uuid.UUID) ([]model.Order, error) {

	result, err := s.repo.GetOrders(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("repository get orders: %w", err)
	}

	return result, nil
}

// UploadOrder реализует бизнес-сценарий загрузки и первичной обработки нового заказа:
// 1. Инициализирует модель и валидирует контрольную сумму номера заказа по алгоритму Луна.
// 2. Выполняет сохранение в базу данных с обработкой возможных коллизий уникальности номеров.
// 3. Асинхронно нотифицирует пул воркеров о появлении новой задачи на расчет.
func (s *OrderService) UploadOrder(ctx context.Context, orderNumStr string, uid uuid.UUID) error {

	order, err := model.NewOrder(orderNumStr, uid).Validate()
	if err != nil {
		return ErrInvalidOrderNum
	}

	if err := s.repo.CreateOrder(ctx, order); err != nil {
		switch {
		case errors.Is(err, orderrepo.ErrOrderAlreadyCreatedByAnotherUser):
			return ErrOrderAlreadyCreatedByAnotherUser
		case errors.Is(err, orderrepo.ErrOrderAlreadyProcessing):
			return ErrOrderAlreadyProcessing
		default:
			return fmt.Errorf("repository create order: %w", err)
		}
	}

	s.notifier.Notify(*order)

	return nil

}
