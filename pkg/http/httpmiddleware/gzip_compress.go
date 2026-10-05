package httpmiddleware

import (
	"net/http"
	"strings"

	"github.com/Sayfargo/gophermart-loyal-service/pkg/http/httpio"
)

// GzipCompress возвращает Middleware, которое автоматически распаковывает входящие запросы
// (если установлен заголовок Content-Encoding: gzip) и сжимает исходящие ответы
// (если клиент передал заголовок Accept-Encoding: gzip и тип контента совпадает с JSON/HTML).
func GzipCompress() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			originalWriter := w

			acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")

			if acceptsGzip {
				gzipWriter := httpio.NewGzipWriter(w)

				originalWriter = gzipWriter

				defer gzipWriter.Close()
			}

			sendsGzip := strings.Contains(r.Header.Get("Content-Encoding"), "gzip")

			if sendsGzip {
				gzipReader, err := httpio.NewGzipReader(r.Body)
				if err != nil {
					http.Error(
						w,
						"invalid gzip body",
						http.StatusBadRequest,
					)
					return
				}

				r.Body = gzipReader
				defer gzipReader.Close()
			}

			next.ServeHTTP(originalWriter, r)
		})
	}
}
