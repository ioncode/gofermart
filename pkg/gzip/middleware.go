package gzip

import (
	"net/http"
	"strings"
)

// Middleware представляет собой промежуточное ПО для HTTP-сервера.
// Оно автоматически сжимает ответы для клиентов, поддерживающих gzip,
// и прозрачно распаковывает входящие запросы с Content-Encoding: gzip.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// По умолчанию работаем с оригинальным http.ResponseWriter
		originalWriter := w

		// Проверяем, поддерживает ли клиент чтение сжатых gzip-данных.
		acceptEncoding := r.Header.Get("Accept-Encoding")
		if strings.Contains(acceptEncoding, "gzip") {
			cw := NewCompressWriter(w)
			originalWriter = cw
			defer cw.Close()
		}

		// Если клиент прислал тело запроса в сжатом виде
		contentEncoding := r.Header.Get("Content-Encoding")
		if strings.Contains(contentEncoding, "gzip") {
			cr, err := NewCompressReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		next.ServeHTTP(originalWriter, r)
	})
}
