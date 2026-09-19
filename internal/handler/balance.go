package handler

import (
	"net/http"

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
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	current, withdrawn, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить баланс пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	response := balanceDTO{
		Current:   current,
		Withdrawn: withdrawn,
	}

	WriteJSONOptimized(w, http.StatusOK, response)
}
