package handler

import (
	"errors"
	"io"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/pkg/luhn"
)

// UploadOrder обрабатывает HTTP-запрос POST /api/user/orders.
func (h *UserHandler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	// Извлекаем userID, который Middleware записала в контекст запроса
	userID, ok := getUserIDFromContext(r.Context())
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
	if !luhn.IsValid(orderID) {
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
	userID, ok := getUserIDFromContext(r.Context())
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

	// 1. Сначала маршалим данные в слайс байт в оперативной памяти Go
	bodyBytes, err := json.Marshal(orders)
	if err != nil {
		// Если произошел сбой, заголовки еще НЕ отправлены!
		// Мы можем честно вернуть статус 500
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError) // 500
		_, _ = w.Write([]byte("Internal Server Error: failed to encode response"))

		h.logger.Error("Ошибка маршалинга списка заказов в JSON", err)
		return
	}

	// 2. Ошибок нет. Теперь со спокойной душой отправляем 200 OK и тело
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200
	_, _ = w.Write(bodyBytes)
}
