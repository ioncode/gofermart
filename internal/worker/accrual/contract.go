package accrual

import (
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/shopspring/decimal"
)

// AccrualResponse описывает структуру JSON-ответа, получаемого
// от внешнего сервиса расчета баллов начислений.
type AccrualResponse struct {
	// Order представляет собой уникальный номер заказа в виде строки.
	Order string `json:"order"`
	// Status содержит текущий статус обработки заказа во внешней системе.
	Status domain.AccrualStatus `json:"status"`
	// Accrual хранит количество начисленных баллов (может быть nil для промежуточных статусов).
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
}
