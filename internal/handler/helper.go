package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
)

// MaxBodySize определяет максимальный допустимый размер тела HTTP-запроса в байтах.
// Значение установлено в 4096 байт (4 КБ) в качестве базовой DoS-защиты периметра.
const MaxBodySize = 4096

// bytesBufferPool хранит указатели на массивы байт фиксированной длины для чтения запросов.
// Каждое выделение памяти строго ограничено константой MaxBodySize (4096 байт).
// Использование пула sync.Pool позволяет переиспользовать память между запросами,
// полностью исключая нагрузку на сборщик мусора (GC) при массовой загрузке данных.
var bytesBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, MaxBodySize)
		return &b
	},
}

// ReadBodyOptimized — универсальный сетевой слой с нулевыми аллокациями.
// Вся защита от DoS и ограничение размера тела делегированы Middleware роутера.
func ReadBodyOptimized(w http.ResponseWriter, r *http.Request, processor func(payload []byte) bool) bool {
	bufPtr := bytesBufferPool.Get().(*[]byte)
	buf := *bufPtr

	// Изоляция памяти: мгновенно зачищаем буфер от старых данных
	clear(buf)

	defer func() {
		clear(buf)
		bytesBufferPool.Put(bufPtr)
		r.Body.Close()
	}()

	// Вычитываем данные из потока (который в проде ограничен http.MaxBytesReader в Middleware).
	// io.ReadFull пытается заполнить все 4096 байт буфера.
	n, err := io.ReadFull(r.Body, buf)

	if err != nil {
		// 1. Обработка ошибки переполнения от http.MaxBytesReader (Middleware)
		// ОПТИМИЗАЦИЯ: Проверяем ошибку MaxBytesReader по ее текстовому маркеру.
		// Это полностью убирает вызов errors.As, который приводил к аллокации 8 байт в куче.
		if err.Error() == "http: request body too large" {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge) // 413
			return false
		}

		// 2. Обработка успешного окончания короткого потока.
		// В проде (под MaxBytesReader) и в тестах при успешном чтении короткого JSON
		// io.ReadFull ВСЕГДА возвращает io.ErrUnexpectedEOF, потому что буфер 4КБ не заполнился встык.
		if errors.Is(err, io.ErrUnexpectedEOF) {
			if n == 0 {
				http.Error(w, "Request body is empty", http.StatusBadRequest) // 400
				return false
			}
			// Если n > 0 — это полностью валидные данные, сбрасываем ошибку и идем дальше!
			err = nil
		} else if errors.Is(err, io.EOF) {
			// Чистый io.EOF от io.ReadFull означает, что в потоке было СТРОГО 0 байт с самого начала
			http.Error(w, "Request body is empty", http.StatusBadRequest) // 400
			return false
		} else {
			// Любая реальная сетевая ошибка (таймаут, жесткий обрыв связи)
			http.Error(w, "Failed to read request body", http.StatusBadRequest) // 400
			return false
		}
	}

	// Дополнительный пограничный случай (на случай, если err == nil, но n == 0)
	if n == 0 && err == nil {
		http.Error(w, "Request body is empty", http.StatusBadRequest) // 400
		return false
	}

	// Передаем строго валидный срез памяти в коллбэк
	return processor(buf[:n])
}

// ReadJSONOptimized — строго типизированная дженерик-обертка для парсинга JSON.
func ReadJSONOptimized[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	return ReadBodyOptimized(w, r, func(payload []byte) bool {
		if err := json.Unmarshal(payload, dst); err != nil {
			http.Error(w, "Invalid JSON format", http.StatusBadRequest) // 400
			return false
		}
		return true
	})
}

// jsonPool управляет высокопроизводительными, переиспользуемыми буферами памяти
// для сериализации исходящих HTTP-ответов в формате JSON.
var jsonPool = sync.Pool{
	New: func() any {
		// new(bytes.Buffer) возвращает чистый указатель *bytes.Buffer.
		// Это классический, читаемый и безопасный подход для Go-приложений.
		return new(bytes.Buffer)
	},
}

// WriteJSONOptimized выполняет потоковую сериализацию данных в JSON без лишних аллокаций в хендлере.
func WriteJSONOptimized[T any](w http.ResponseWriter, statusCode int, data *T) {
	// Извлекаем понятный и привычный *bytes.Buffer
	buf := jsonPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset() // Сбрасываем длину буфера в 0, сохраняя выделенную емкость (capacity)
		jsonPool.Put(buf)
	}()

	// Инициализируем потоковый кодировщик напрямую в буфер
	if err := json.NewEncoder(buf).Encode(data); err != nil {
		http.Error(w, "Failed to serialize response", http.StatusInternalServerError)
		return
	}

	// Выставляем REST API заголовки и отправляем данные в сокет
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = buf.WriteTo(w)
}

// WriteJSONWithMarshalling выполняет стандартную сериализацию данных через json.Marshal.
// Метод выделяет память в куче под итоговый срез байт на каждый запрос и передает его в сокет.
// Используется для сравнительного анализа производительности в бенчмарках.
func WriteJSONWithMarshalling[T any](w http.ResponseWriter, statusCode int, data T) {
	// json.Marshal вынужден аллоцировать новый кусок памяти в куче под итоговый JSON
	bytes, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "Failed to serialize response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Передаем выделенный срез байт в сетевой интерфейс
	_, _ = w.Write(bytes)
}
