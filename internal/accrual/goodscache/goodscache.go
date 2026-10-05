// Package goodscache предоставляет потокобезопасный кэш в оперативной памяти
// для хранения и быстрого доступа к правилам начисления вознаграждений за товары.
package goodscache

import (
	"context"
	"sync"

	goodsmodel "github.com/Sayfargo/gophermart-loyal-service/internal/accrual/goods/model"
)

// GoodsCache представляет собой потокобезопасную обертку над срезом правил вознаграждений,
// защищенную с помощью sync.RWMutex для конкурентного чтения и записи.
type GoodsCache struct {
	mu    sync.RWMutex
	rules []goodsmodel.GoodsInfo

	provider GoodsRrovider
}

// GoodsRrovider определяет интерфейс для первоначальной загрузки правил вознаграждений
// из базы данных.
type GoodsRrovider interface {
	GetAllGoods(ctx context.Context) ([]goodsmodel.GoodsInfo, error)
}

// New создает и инициализирует новый экземпляр GoodsCache с пустым срезом правил
// и установленным провайдером данных.
func New(goodsRepo GoodsRrovider) *GoodsCache {
	return &GoodsCache{
		rules:    []goodsmodel.GoodsInfo{},
		provider: goodsRepo,
	}
}

// Get возвращает изолированную копию текущего среза правил вознаграждений.
// Использует разделяемую блокировку (RLock) для безопасного конкурентного чтения.
func (c *GoodsCache) Get() []goodsmodel.GoodsInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rules := make([]goodsmodel.GoodsInfo, len(c.rules))
	copy(rules, c.rules)
	return rules
}

// Set полностью перезаписывает текущий срез правил вознаграждений новым набором данных.
// Использует эксклюзивную блокировку (Lock) для предотвращения состояния гонки (race conditions).
func (c *GoodsCache) Set(rules []goodsmodel.GoodsInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = rules
}

// Load загружает все правила вознаграждений из провайдера данных (базы данных)
// и атомарно сохраняет их в кэш. Обычно вызывается один раз при старте приложения (warm-up).
func (c *GoodsCache) Load(ctx context.Context) error {
	rules, err := c.provider.GetAllGoods(ctx)
	if err != nil {
		return err
	}
	c.Set(rules)
	return nil
}

// Add добавляет одно новое правило вознаграждения в конец текущего среза кэша.
// Использует эксклюзивную блокировку (Lock) для потокобезопасного расширения среза.
func (c *GoodsCache) Add(rule goodsmodel.GoodsInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = append(c.rules, rule)
}
