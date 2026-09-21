package gzip

import (
	"log"
	"net/http"
	"strings"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Распаковываем только POST/PUT запросы с Content-Encoding: gzip
		contentEncoding := r.Header.Get("Content-Encoding")
		if (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.Contains(contentEncoding, "gzip") {
			cr, err := NewCompressReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		// Упаковываем, если клиент поддерживает сжатие gzip
		acceptEncoding := r.Header.Get("Accept-Encoding")
		log.Println(acceptEncoding)
		if strings.Contains(acceptEncoding, "gzip") {
			cw := NewCompressWriter(w)

			defer cw.Close()

			next.ServeHTTP(cw, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}
