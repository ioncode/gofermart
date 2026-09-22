package handler

import (
	"bytes"
	"errors"
	"net/http"

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
		// Чистим пробельные символы прямо в срезе байт без аллокаций
		cleanedPayload := bytes.TrimSpace(payload)

		if len(cleanedPayload) > 0 {
			// выделяем строку только если срез байт после обрезки мусора содержит полезную нагрузку (не пустой)
			orderID = string(cleanedPayload)
			return true
		}

		// для невалидного запроса сразу возвращаем ошибку
		return false
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
// GetOrders возвращает JSON-список всех заказов авторизованного пользователя.
func (h *UserHandler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized user context missing", http.StatusUnauthorized) // 401
		return
	}

	orders, err := h.service.GetOrders(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить список заказов пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent) // 204
		return
	}

	WriteJSONOptimized(w, http.StatusOK, &orders)
}
