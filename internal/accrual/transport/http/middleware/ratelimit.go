package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
)

// RateLimiter определяет интерфейс для инкрементирования счетчиков запросов,
// который должен быть реализован потокобезопасным хранилищем лимитов.
type RateLimiter interface {
	// Incr увеличивает значение счетчика для заданного ключа в рамках временного окна.
	// Возвращает текущее количество запросов и время окончания действия окна.
	Incr(key string, window time.Duration) (int, time.Time)
}

// GlobalRateLimit возвращает функцию промежуточного слоя (middleware), которая ограничивает
// суммарное число запросов ко всему HTTP-сервису на основе переданной конфигурации cfg.
// В случае превышения лимита возвращает клиенту статус 429 Too Many Requests и выставляет
// заголовок Retry-After с количеством секунд, оставшихся до сброса текущего временного окна.
func GlobalRateLimit(limiter RateLimiter, cfg ratelimitstore.RateLimitConfig, keyPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := fmt.Sprintf("rate:%s:global", keyPrefix)
			count, exp := limiter.Incr(key, cfg.Window)

			if count > cfg.MaxRequests {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", time.Until(exp).Seconds()))
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
