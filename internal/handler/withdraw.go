package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

type WithdrawRequest struct {
	Order string          `json:"order"`
	Sum   decimal.Decimal `json:"sum"`
}

func (h *UserHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	// Достаем userID из приватного хелпера контекста
	userID, ok := getUserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized) // 401
		return
	}
	// Создаем контекстный логгер для отслеживания финансовой операции
	ctxLogger := h.logger.With(ulog.String("user_id", userID))

	var req WithdrawRequest
	if !ReadJSONOptimized(w, r, &req) {
		return
	}

	req.Order = strings.TrimSpace(req.Order)
	if req.Order == "" || req.Sum.IsNegative() || req.Sum.IsZero() {
		http.Error(w, "Invalid request payload", http.StatusBadRequest) // 400
		return
	}
	// Добавляем номер заказа в логгер для сквозного отслеживания
	ctxLogger = ctxLogger.With(ulog.String("order_id", req.Order), ulog.String("amount", req.Sum.String()))

	// Вызываем бизнес-логику списания
	err := h.service.Withdraw(r.Context(), userID, req.Order, req.Sum)
	if err != nil {
		ctxLogger.Error("Ошибка при попытке списания баллов", err)
		if errors.Is(err, service.ErrInvalidOrderNumber) {
			http.Error(w, "Invalid order number", http.StatusUnprocessableEntity) // 422
			return
		}
		if errors.Is(err, repository.ErrInsufficientFunds) {
			http.Error(w, "Insufficient funds", http.StatusPaymentRequired) // 402
			return
		}

		http.Error(w, "Internal server error", http.StatusInternalServerError) // 500
		return
	}
	ctxLogger.Info("Баллы успешно списаны в счет нового заказа")

	w.WriteHeader(http.StatusOK) // 200
}
