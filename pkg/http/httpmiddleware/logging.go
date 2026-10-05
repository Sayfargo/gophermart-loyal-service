package httpmiddleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/http/httpio"
)

// Logging возвращает Middleware, которое логирует детали входящего HTTP-запроса и исходящего ответа.
// Логирует URI, метод, User-Agent, Request ID, а также итоговый статус-код, время обработки (latency) и размер тела ответа.
func Logging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(requestHeader)
			rw := httpio.NewLoggingResponseWriter(w)
			start := time.Now()

			l := log.With(
				slog.String("request_id", requestID),
				slog.String("user_agent", r.Header.Get("User-Agent")),
				slog.String("URI", r.RequestURI),
				slog.String("method", r.Method),
			)

			l.Info("HTTP Request started")

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			l.Info(
				"HTTP Request done",
				slog.Int("status", rw.GetStatusCode()),
				slog.Duration("latency", duration),
				slog.Int("size", rw.GetBodySize()),
			)
		})
	}
}
