package handler

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/pkg/luhn"
)

// UploadOrder обрабатывает HTTP-запрос POST /api/user/orders согласно спецификации ТЗ.
func (h *UserHandler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "User unauthorized", http.StatusUnauthorized) // 401
		return
	}

	var orderID string
	ok = ReadBodyOptimized(w, r, func(payload []byte) bool {
		orderID = strings.TrimSpace(string(payload))
		return true // Чтение прошло успешно, бизнес-логику выполняем дальше по хендлеру
	})
	if !ok {
		return // Ошибка уже отправлена внутри ReadBodyOptimized
	}

	if !luhn.IsValid(orderID) {
		http.Error(w, "Invalid order number format (Luhn check failed)", http.StatusUnprocessableEntity) // 422
		return
	}

	err := h.service.UploadOrder(r.Context(), userID, orderID)
	if err != nil {
		if errors.Is(err, service.ErrOrderUploadedBySameUser) {
			w.WriteHeader(http.StatusOK) // 200
			return
		}
		if errors.Is(err, service.ErrOrderUploadedByOtherUser) {
			http.Error(w, "Order already uploaded by another user", http.StatusConflict) // 409
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError) // 500
		return
	}

	w.WriteHeader(http.StatusAccepted) // 202
}

// GetOrders возвращает JSON-список всех заказов авторизованного пользователя.
// Хендлер привязан к маршруту: GET /api/user/orders
func (h *UserHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized user context missing", http.StatusUnauthorized)
		return
	}

	orders, err := h.service.GetOrders(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить список заказов пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 1. Достаем буфер из пула
	bufPtr := bytesBufferPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	defer func() {
		if cap(buf) >= MaxBodySize+1 {
			*bufPtr = buf
		}
		bytesBufferPool.Put(bufPtr)
	}()

	// 2. Объявляем структуру строго НА СТЕКЕ (без NewBuffer)
	var respBuffer bytes.Buffer

	// Подкладываем под нее наш переиспользуемый массив байт
	respBuffer.Write(buf)

	// 3. Кодируем через NewEncoder.
	// Так как respBuffer передается по адресу &respBuffer,
	// но сама структура живет на стеке, это работает максимально эффективно.
	if err := json.NewEncoder(&respBuffer).Encode(orders); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
		h.logger.Error("Ошибка кодирования списка заказов в JSON", err)
		return
	}

	// Забираем итоговые байты для записи в сеть
	buf = respBuffer.Bytes()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}
