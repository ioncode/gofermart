package handler

import (
	"net/http"

	json "github.com/goccy/go-json"
	"github.com/shopspring/decimal"
)

// balanceDTO описывает транспортную структуру ответа API баланса.
// Находится в слое хендлеров для изоляции логики отображения JSON.
type balanceDTO struct {
	Current   decimal.Decimal `json:"current"`
	Withdrawn decimal.Decimal `json:"withdrawn"`
}

// GetBalance возвращает JSON с текущим балансом и списаниями авторизованного пользователя.
// Хендлер привязан к маршруту: GET /api/user/balance
func (h *UserHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed) // 405
		return
	}

	// Извлекаем userID из контекста авторизации (Middleware)
	userID, ok := r.Context().Value(UserIDContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized) // 401
		return
	}

	// Получаем чистые децималы из слоя бизнес-логики
	current, withdrawn, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить баланс пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	// Собираем DTO-структуру специально для JSON маршалинга
	response := balanceDTO{
		Current:   current,
		Withdrawn: withdrawn,
	}

	// Безопасный маршалинг в память до отправки статус-кода 200
	bodyBytes, err := json.Marshal(response)
	if err != nil {
		h.logger.Error("Ошибка маршалинга баланса в JSON", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	// Успешный ответ
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200
	_, _ = w.Write(bodyBytes)
}
