package handler

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

// WithdrawRequest описывает структуру входящего JSON-пакета
// для совершения финансовой операции списания бонусных баллов.
type WithdrawRequest struct {
	Order string          `json:"order"` // Номер заказа, в счет которого списываются баллы
	Sum   decimal.Decimal `json:"sum"`   // Сумма списываемых баллов
}

// Reset выполняет сброс всех полей структуры WithdrawRequest до их нулевых значений.
//
// Метод необходим для очистки объекта перед его возвращением в [withdrawRequestPool],
// что гарантирует отсутствие загрязнения данных (Data Contamination) и исключает
// протекание финансовых сумм между параллельными HTTP-запросами разных горутину.
func (r *WithdrawRequest) Reset() {
	r.Order = ""
	r.Sum = decimal.Zero
}

// withdrawRequestPool представляет собой потокобезопасный пул объектов [sync.Pool]
// для переиспользования памяти, выделенной под структуры [WithdrawRequest].
//
// Архитектурное назначение:
//   - Позволяет зафиксировать объекты в памяти и минимизировать количество аллокаций
//     в куче (heap) при частых финансовых запросах на списание баллов.
//   - Защищает сборщик мусора (GC) от дополнительной нагрузки на высоконагруженных эндпоинтах.
var withdrawRequestPool = sync.Pool{
	New: func() any {
		// Выделяем память под структуру один раз при инициализации элемента пула
		return new(WithdrawRequest)
	},
}

// Withdraw обрабатывает HTTP-запрос POST /api/user/balance/withdraw на списание баллов.
//
// Функция выполняет списание бонусных баллов авторизованного пользователя в счет
// оплаты нового заказа согласно техническому заданию системы лояльности.
//
// Архитектурные особенности:
//   - Для минимизации нагрузки на сборщик мусора (GC) структура запроса [WithdrawRequest]
//     извлекается из пула объектов [withdrawRequestPool] и очищается через метод Reset() в defer.
//   - Чтение и десериализация входящего JSON выполняются через высокопроизводительный
//     дженерик-хелпер [ReadJSONOptimized] с эффективностью Zero-Allocation на сетевом уровне.
//
// Возвращаемые HTTP-статусы:
//   - 200 OK: баллы успешно списаны в счет нового заказа.
//   - 401 Unauthorized: пользователь не авторизован (отсутствует userID в контексте).
//   - 400 Bad Request: неверный формат JSON или некорректные значения полей (пустой заказ, отрицательная сумма).
//   - 422 Unprocessable Entity: передан некорректный номер заказа (не прошел проверку алгоритма Луна).
//   - 402 Payment Required: на счету пользователя недостаточно средств для списания.
//   - 500 Internal Server Error: непредвиденная ошибка на уровне бизнес-логики или базы данных.
func (h *UserHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized) // 401
		return
	}

	ctxLogger := h.logger.With(ulog.String("user_id", userID))

	// 1. Извлекаем чистый объект из пула для снижения аллокаций в куче
	req := withdrawRequestPool.Get().(*WithdrawRequest)

	// 2. Гарантируем очистку полей и возврат структуры в пул после обработки запроса
	defer func() {
		req.Reset()
		withdrawRequestPool.Put(req)
	}()

	// 3. Парсим JSON с использованием дженерик-версии хелпера
	if !ReadJSONOptimized(w, r, req) {
		return
	}

	req.Order = strings.TrimSpace(req.Order)
	if req.Order == "" || req.Sum.IsNegative() || req.Sum.IsZero() {
		http.Error(w, "Invalid request payload", http.StatusBadRequest) // 400
		return
	}

	ctxLogger = ctxLogger.With(ulog.String("order_id", req.Order), ulog.String("amount", req.Sum.String()))

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
