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

// ReadBodyOptimized теперь принимает коллбэк processor.
// Срез байт не "убегает" из функции, что гарантирует 0 аллокаций в куче.
func ReadBodyOptimized(w http.ResponseWriter, r *http.Request, processor func(payload []byte) error) bool {

	// 2. Берем буфер из пула
	bufPtr := bodyBufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer bodyBufferPool.Put(bufPtr)
	defer r.Body.Close()

	// 3. Чтение потока
	n, err := io.ReadFull(r.Body, buf)
	if n > MaxBodySize {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return false
	}

	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		if errors.Is(err, io.EOF) {
			http.Error(w, "Request body is empty", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
		}
		return false
	}

	// 4. Передаем срез во внутренний обработчик.
	// Так как мы находимся внутри функции, Slice Header не аллоцируется в куче!
	if err := processor(buf[:n]); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return false
	}

	return true
}

// ReadJSONOptimized — высокоуровневая обертка, которая объединяет
// Zero-Alloc чтение тела запроса и его последующую десериализацию
func ReadJSONOptimized(w http.ResponseWriter, r *http.Request, dst any) bool {
	// 1. Валидация заголовка
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	// 2. Передаем логику десериализации как коллбэк внутрь сетевого этапа
	return ReadBodyOptimized(w, r, func(payload []byte) error {
		return json.Unmarshal(payload, dst)
	})
}
