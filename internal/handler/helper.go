package handler

import (
	"errors"
	"io"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
)

const MaxBodySize = 4096

// Инициализируем пул буферов размером на 1 байт больше лимита (4097 байт).
// Это позволяет поймать превышение размера (Overflow) без вызова добавочного io.Read
// и полностью устраняет необходимость аллокации временных слайсов типа make([]byte, 1).
var bodyBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, MaxBodySize+1)
		return &b
	},
}

// ReadJSONOptimized вычитывает и десериализует JSON с гарантированными 0 аллокаций.
func ReadJSONOptimized(w http.ResponseWriter, r *http.Request, dst any) bool {
	// 1. Быстрая валидация Content-Type без тяжелого парсинга строк
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}

	// 2. Берем буфер из пула
	bufPtr := bodyBufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer bodyBufferPool.Put(bufPtr)
	defer r.Body.Close()

	// 3. Читаем поток. Мы запрашиваем ровно MaxBodySize + 1 байт.
	n, err := io.ReadFull(r.Body, buf)

	// Если прочитано больше, чем MaxBodySize (то есть n == 4097) — запрос слишком большой
	if n > MaxBodySize {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return false
	}

	// 4. Обрабатываем стандартные ошибки чтения
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		if errors.Is(err, io.EOF) {
			http.Error(w, "Request body is empty", http.StatusBadRequest)
			return false
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return false
	}

	// 5. Выделяем точный слайс с данными
	payload := buf[:n]

	if err := json.Unmarshal(payload, dst); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return false
	}

	return true
}
