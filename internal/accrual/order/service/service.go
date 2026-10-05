// Package service предоставляет слой бизнес-логики для управления заказами,
// включая их валидацию, регистрацию в постоянном хранилище и отправку уведомлений воркеру для расчета баллов.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/model"
	orderrepo "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/order/repository"
)

// OrderNotifier определяет интерфейс для асинхронного оповещения фоновых систем (воркеров)
// о появлении нового зарегистрированного заказа, готового к расчету вознаграждений.
type OrderNotifier interface {
	// Notify передает заказ в систему обработки и вычисления баллов лояльности.
	Notify(order model.Order)
}

// OrderRepository определяет интерфейс взаимодействия со слоем персистентного хранения данных заказов.
type OrderRepository interface {
	// CreateOrder выполняет атомарное сохранение структуры заказа и его товарных позиций.
	CreateOrder(ctx context.Context, order *model.Order) error
	// GetOrder осуществляет поиск и извлечение информации о заказе по его строковому номеру.
	GetOrder(ctx context.Context, orderNum string) (model.Order, error)
}

// OrderService инкапсулирует репозиторий, логгер и систему нотификации
// для обеспечения централизованных бизнес-сценариев работы с заказами.
type OrderService struct {
	repo     OrderRepository
	logger   *slog.Logger
	notifier OrderNotifier
}

var (
	// ErrInvalidOrderNum возвращается сервисным слоем, если номер заказа не прошел валидацию (алгоритм Луна).
	ErrInvalidOrderNum = errors.New("invalid order number")
	// ErrOrderAlreadyProcessing возвращается, если заказ с данным номером уже был ранее загружен в систему.
	ErrOrderAlreadyProcessing = errors.New("order has already been uploaded by user")
	// ErrOrdersNotFound возвращается, если искомые заказы отсутствуют в базе данных.
	ErrOrderNotFound = errors.New("order not found")
)

// New создает и инициализирует новый экземпляр OrderService с необходимыми внешними зависимостями.
func New(
	repo OrderRepository,
	log *slog.Logger,
	notifier OrderNotifier,
) *OrderService {
	return &OrderService{
		repo:     repo,
		logger:   log,
		notifier: notifier,
	}
}

// GetOrder запрашивает информацию о конкретном заказе по его уникальному номеру.
// Метод извлекает данные из репозитория и выполняет маппинг ошибок:
//   - Если заказ не найден в базе данных, возвращается доменная ошибка ErrOrderNotFound;
//   - При возникновении технических неполадок с репозиторием ошибка оборачивается контекстом.
func (s *OrderService) GetOrder(ctx context.Context, orderNum string) (model.Order, error) {

	order, err := s.repo.GetOrder(ctx, orderNum)
	if err != nil {
		if errors.Is(err, orderrepo.ErrOrderNotFound) {
			return model.Order{}, ErrOrderNotFound
		}
		return model.Order{}, fmt.Errorf("repo get order: %w", err)
	}

	return order, nil
}

// UploadOrder выполняет полный бизнес-сценарий регистрации нового заказа:
// 1. Создает доменную модель и валидирует корректность контрольной суммы номера заказа.
// 2. Персистентно сохраняет заказ через репозиторий.
// 3. Отправляет асинхронное уведомление через notifier для немедленного начисления баллов воркером.
func (s *OrderService) UploadOrder(ctx context.Context, order model.Order) error {

	newOrder, err := model.NewOrder(order.OrderNum, order.Goods).Validate()
	if err != nil {
		return ErrInvalidOrderNum
	}

	if err = s.repo.CreateOrder(ctx, newOrder); err != nil {
		switch {
		case errors.Is(err, orderrepo.ErrOrderAlreadyProcessing):
			return ErrOrderAlreadyProcessing
		default:
			return fmt.Errorf("repository create order: %w", err)
		}
	}

	s.notifier.Notify(*newOrder)

	return nil
}
