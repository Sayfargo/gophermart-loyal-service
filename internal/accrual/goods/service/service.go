// Package service предоставляет слой бизнес-логики для управления правилами вознаграждений за товары.
package service

import (
	"context"
	"fmt"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
)

// GoodsRepository определяет интерфейс для взаимодействия со слоем постоянного хранения данных товаров.
type GoodsRepository interface {
	// RegisterGoods сохраняет новое правило вознаграждения в постоянное хранилище.
	RegisterGoods(ctx context.Context, goods model.GoodsInfo) error
}

// GoodsCacheAdder определяет интерфейс для динамического добавления правил в кэш оперативной памяти.
type GoodsCacheAdder interface {
	// Add регистрирует или обновляет правило вознаграждения внутри кэша.
	Add(rule model.GoodsInfo)
}

// GoodsService управляет бизнес-процессами, связанными с валидацией,
// персистентным сохранением и синхронизацией правил вознаграждений в кэше.
type GoodsService struct {
	repo       GoodsRepository
	goodsCache GoodsCacheAdder
}

// New создает и инициализирует новый экземпляр GoodsService с необходимыми зависимостями.
func New(goodsRepo GoodsRepository, goodsCache GoodsCacheAdder) *GoodsService {
	return &GoodsService{
		repo:       goodsRepo,
		goodsCache: goodsCache,
	}
}

// RegisterGoods выполняет сквозной процесс регистрации нового правила вознаграждения.
// Сначала проверяет корректность данных, затем сохраняет их в базу данных,
// и при успешном сохранении обновляет локальный кэш в оперативной памяти.
func (s *GoodsService) RegisterGoods(ctx context.Context, goods model.GoodsInfo) error {
	if err := goods.Validate(); err != nil {
		return err
	}

	if err := s.repo.RegisterGoods(ctx, goods); err != nil {
		return fmt.Errorf("register goods: %w", err)
	}

	s.goodsCache.Add(goods)

	return nil
}
