// Пакет domain содержит основные бизнес-сущности и доменные модели
// накопительной системы лояльности «Гофермарт».
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Withdrawal представляет собой доменную модель операции списания баллов
// с накопительного счета пользователя в счет оплаты нового заказа.
type Withdrawal struct {
	// Order содержит уникальный номер заказа, на который списываются баллы.
	Order string `json:"order"`
	// Sum отражает точную сумму списания баллов лояльности,
	// валидированную через тип данных высокой точности decimal.Decimal.
	Sum decimal.Decimal `json:"sum"`
	// ProcessedAt фиксирует точную дату и время успешного проведения списания.
	ProcessedAt time.Time `json:"processed_at"`
}
