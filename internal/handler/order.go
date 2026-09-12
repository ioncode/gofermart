package handler

import (
	"errors"
	"io"
	"net/http"
	"strings"

	// Путь к алгоритму Луна
	// Путь к ошибкам репозитория/сервиса

	"github.com/ioncode/gofermart/internal/service"
)

// UploadOrder обрабатывает HTTP-запрос POST /api/user/orders.
// Хендлер принимает номер заказа в формате text/plain, валидирует по алгоритму Луна
// и отправляет в монолитный LoyaltyService для асинхронной обработки.
func (h *UserHandler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	// 1. Проверяем HTTP метод
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. Проверяем Content-Type (спецификация строго требует text/plain)
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		http.Error(w, "Invalid Content-Type, expected text/plain", http.StatusBadRequest) // 400
		return
	}

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

	// 6. Передаем строго типизированные аргументы в монолитный сервис автора
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
