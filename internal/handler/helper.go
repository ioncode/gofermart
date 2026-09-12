package handler

import (
	"errors"
	"io"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
)

const MaxBodySize = 4096

// Инициализируем пул байтовых буферов fixed-размера (4 КБ).
// Это уберет аллокации из io.ReadAll и защитит GC от нагрузки.
var bodyBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, MaxBodySize)
		return &b
	},
}

func ReadJSONOptimized(w http.ResponseWriter, r *http.Request, dst any) bool {
	// 1. Быстрая проверка Content-Type без вызова тяжелых функций strings
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}

	// 2. Берем готовый буфер из пула (уменьшает аллокации до 0)
	bufPtr := bodyBufferPool.Get().(*[]byte)
	buf := *bufPtr

	// Гарантируем возврат буфера в пул по завершении работы функции
	defer bodyBufferPool.Put(bufPtr)
	defer r.Body.Close()

	// 3. Вычитываем данные напрямую в пуленный буфер
	// io.ReadFull читает ровно столько, сколько вмещает буфер, либо пока поток не кончится (io.EOF)
	n, err := io.ReadFull(r.Body, buf)

	// Если err == io.ErrUnexpectedEOF, значит поток закрылся до заполнения 4КБ (это штатное поведение для мелких JSON)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		// Если ошибки io.EOF нет, а err == nil — значит данных пришло больше, чем 4КБ (наш лимит)
		if err == nil {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return false
	}

	// n — это реальное количество прочитанных байт. Отсекаем слайс до этого размера.
	payload := buf[:n]

	// 4. Парсим JSON с помощью сверхбыстрого goccy
	if err := json.Unmarshal(payload, dst); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return false
	}

	return true
}

// IsValidLuhn проверяет строку цифр на корректность по алгоритму Луна
func IsValidLuhn(number string) bool {
	if len(number) == 0 {
		return false
	}

	var sum int
	parity := len(number) % 2

	for i, r := range number {
		if r < '0' || r > '9' {
			return false // Номер должен состоять только из цифр
		}

		digit := int(r - '0')
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}

	return sum%10 == 0
}
