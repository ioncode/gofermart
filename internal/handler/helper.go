package handler

import (
	"errors"
	"io"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
)

const MaxBodySize = 4096

// Универсальный пул буферов.
// Хранит указатели на слайсы с длиной 0, но емкостью 4097 байт.
var bytesBufferPool = sync.Pool{
	New: func() any {
		// len = 0, cap = 4097
		b := make([]byte, 0, MaxBodySize+1)
		return &b
	},
}

// ReadBodyOptimized теперь принимает коллбэк processor.
// Срез байт не "убегает" из функции, что гарантирует 0 аллокаций в куче.
func ReadBodyOptimized(w http.ResponseWriter, r *http.Request, processor func(payload []byte) bool) bool {

	// 2. Берем буфер из пула
	bufPtr := bytesBufferPool.Get().(*[]byte)
	// Безопасность: проверяем, хватает ли емкости буфера из пула для чтения тела запроса
	if cap(*bufPtr) < MaxBodySize+1 {
		// Если пул вернул маленький буфер, перевыделяем его до безопасного размера
		*bufPtr = make([]byte, MaxBodySize+1)
	}
	// Расширяем длину слайса до максимума для io.ReadFull
	buf := (*bufPtr)[:MaxBodySize+1]
	defer func() {
		bytesBufferPool.Put(bufPtr)
		r.Body.Close()
	}()

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
	return processor(buf[:n])
}

// ReadJSONOptimized — высокоуровневая обертка, которая объединяет
// Zero-Alloc чтение тела запроса и его последующую десериализацию.
// Параметр [T any] позволяет компилятору работать с конкретным типом структуры без аллокаций интерфейса any.
func ReadJSONOptimized[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}

	// Передаем логику десериализации как коллбэк внутрь сетевого этапа.
	// Так как dst имеет конкретный тип *T, goccy/go-json парсит данные с максимальной скоростью.
	return ReadBodyOptimized(w, r, func(payload []byte) bool {
		if err := json.Unmarshal(payload, dst); err != nil {
			http.Error(w, "Invalid JSON format", http.StatusBadRequest)
			return false
		}
		return true
	})
}
