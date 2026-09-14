package handler

import (
	"errors"
	"io"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/service"
)

// UploadOrder обрабатывает HTTP-запрос POST /api/user/orders.
func (h *UserHandler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	// Извлекаем userID, который Middleware записала в контекст запроса
	userID, ok := r.Context().Value(UserIDContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, "User unauthorized", http.StatusUnauthorized) // 401
		return
	}

	// 4. Ограничиваем размер тела запроса (защита от DoS, номер заказа не может быть огромным)
	// Используем лимит в 128 байт, что с запасом перекрывает любые номера заказов
	r.Body = http.MaxBytesReader(w, r.Body, 128)
	defer r.Body.Close()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// Если тело превысило 128 байт, MaxBytesReader вернет ошибку
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge) // 413
			return
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest) // 400
		return
	}

	// Очищаем номер от случайных пробелов или переносов строк
	orderID := strings.TrimSpace(string(bodyBytes))

	// 5. Проверяем валидность номера заказа по алгоритму Луна
	if !IsValidLuhn(orderID) {
		http.Error(w, "Invalid order number format (Luhn check failed)", http.StatusUnprocessableEntity) // 422
		return
	}

	// 6. Передаем строго типизированные аргументы в монолитный сервис
	err = h.service.UploadOrder(r.Context(), userID, orderID)
	if err != nil {
		// Маппинг ошибок согласно требованиям ТЗ
		if errors.Is(err, service.ErrOrderUploadedBySameUser) {
			w.WriteHeader(http.StatusOK) // 200 — номер заказа уже был загружен этим пользователем
			return
		}
		if errors.Is(err, service.ErrOrderUploadedByOtherUser) {
			http.Error(w, "Order already uploaded by another user", http.StatusConflict) // 409
			return
		}

		// Любые непредвиденные системные ошибки (сбой базы, сетевой таймаут)
		http.Error(w, "Internal server error", http.StatusInternalServerError) // 500
		return
	}

	// 7. Спецификация: новый номер заказа принят в обработку
	w.WriteHeader(http.StatusAccepted) // 202
}

// GetOrders возвращает JSON-список всех заказов авторизованного пользователя.
// Хендлер привязан к маршруту: GET /api/user/orders
func (h *UserHandler) GetOrders(w http.ResponseWriter, r *http.Request) {

	// Извлекаем userID, сохраненный Middleware авторизации в контексте запроса
	userID, ok := r.Context().Value(UserIDContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized user context missing", http.StatusUnauthorized) // 401
		return
	}

	// Запрашиваем список заказов из бизнес-логики
	orders, err := h.service.GetOrders(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить список заказов пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	// ТЗ: Если у пользователя нет загруженных заказов, возвращаем 204 No Content
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent) // 204
		return
	}

	// Устанавливаем заголовок контента перед записью тела ответа
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200

	// Сериализуем слайс заказов в JSON напрямую в поток ответа
	if err := json.NewEncoder(w).Encode(orders); err != nil {
		h.logger.Error("Ошибка маршалинга списка заказов в JSON", err)
	}
}
