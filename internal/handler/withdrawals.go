package handler

import (
	"net/http"

	"github.com/ioncode/ulog/v3"
)

// GetWithdrawals возвращает список успешных списаний баллов лояльности авторизованного пользователя.
//
// Хендлер привязан к маршруту: GET /api/user/withdrawals.
// Метод извлекает ID пользователя из контекста, обращается к слою бизнес-логики и,
// при наличии истории списаний, выполняет сериализацию данных через оптимизированный
// дженерик-хелпер WriteJSONOptimized на базе sync.Pool.
//
// Возвращаемые HTTP-статусы:
//   - 200 OK: Список успешно сформирован и передан в виде JSON-массива.
//   - 204 No Content: У пользователя еще нет ни одного зафиксированного списания.
//   - 401 Unauthorized: Пользователь не авторизован или токен сессии невалиден.
//   - 500 Internal Server Error: Непредвиденный сбой базы данных или сервиса.
func (h *UserHandler) GetWithdrawals(w http.ResponseWriter, r *http.Request) {
	// 1. Извлекаем userID из контекста авторизации, установленного AuthMiddleware
	userID, ok := getUserIDFromContext(r.Context())
	if !ok || userID == "" {
		http.Error(w, "Unauthorized user context missing", http.StatusUnauthorized)
		return
	}

	// 2. Создаем контекстный логгер, обогащенный идентификатором пользователя
	ctxLogger := h.logger.With(ulog.String("user_id", userID))

	// 3. Получаем историю списаний из транзакционного слоя бизнес-логики сервиса
	withdrawals, err := h.service.GetWithdrawals(r.Context(), userID)
	if err != nil {
		ctxLogger.Error("Не удалось получить список списаний пользователя", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. По спецификации: если записей нет, возвращаем статус 204
	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 5. Стримим JSON-массив списаний напрямую в TCP-сокет через пул буферов
	WriteJSONOptimized(w, http.StatusOK, &withdrawals)
}
