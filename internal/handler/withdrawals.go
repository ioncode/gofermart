package handler

import (
	"net/http"

	json "github.com/goccy/go-json"
	"github.com/ioncode/ulog/v3"
)

func (h *UserHandler) GetWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized user context missing", http.StatusUnauthorized) // 401
		return
	}

	// Создаем контекстный логгер, привязанный к текущему пользователю
	ctxLogger := h.logger.With(ulog.String("user_id", userID))

	withdrawals, err := h.service.GetWithdrawals(r.Context(), userID)
	if err != nil {
		ctxLogger.Error("Не удалось получить список списаний пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent) // 204
		return
	}

	bodyBytes, err := json.Marshal(withdrawals)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError) // 500
		_, _ = w.Write([]byte("Internal Server Error: failed to encode response"))
		ctxLogger.Error("Ошибка маршалинга списка списаний в JSON", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200
	_, _ = w.Write(bodyBytes)
}
