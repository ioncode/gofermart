// Пакет handler содержит обработчики входящих HTTP-запросов и веб-интерфейсы
// накопительной системы лояльности «Гофермарт».
package handler

import (
	"net/http"
)

// GetBalance возвращает JSON-ответ с текущим балансом и историей списаний авторизованного пользователя.
//
// Хендлер обрабатывает GET-запросы на маршруте /api/user/balance. Метод извлекает идентификатор
// пользователя из контекста выполнения, запрашивает агрегированные данные из слоя бизнес-логики
// и стримит их в сокет с помощью пулируемого Zero-Alloc хелпера WriteJSONOptimized.
//
// Возвращаемые HTTP-статусы:
//   - 200 OK: Запрос успешно обработан, тело ответа содержит JSON со структурой баланса.
//   - 401 Unauthorized: Пользователь не прошел проверку подлинности в AuthMiddleware.
//   - 500 Internal Server Error: Произошел системный сбой при обращении к базе данных.
func (h *UserHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed) // 405
		return
	}

	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized) // 401
		return
	}

	current, withdrawn, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		h.logger.Error("Не удалось получить баланс пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError) // 500
		return
	}

	response := balanceDTO{
		Current:   current,
		Withdrawn: withdrawn,
	}

	WriteJSONOptimized(w, http.StatusOK, &response)
}
