// Пакет domain содержит основные бизнес-сущности и доменные модели
// накопительной системы лояльности «Гофермарт».
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// OrderStatus описывает внутренний статус обработки заказа в нашей системе.
type OrderStatus string

const (
	// StatusNew означает, что заказ успешно загружен пользователем,
	// но фоновый воркер еще не начал его проверку во внешней системе.
	StatusNew OrderStatus = "NEW"

	// StatusProcessing означает, что внешняя система начислений
	// взяла заказ в расчет, и баллы лояльности находятся в процессе вычисления.
	StatusProcessing OrderStatus = "PROCESSING"

	// StatusInvalid является финальным статусом и означает, что внешняя
	// система расчета баллов признала данный номер заказа невалидным.
	StatusInvalid OrderStatus = "INVALID"

	// StatusProcessed является финальным статусом и означает, что расчет
	// баллов лояльности успешно завершен, а начисления зафиксированы на балансе.
	StatusProcessed OrderStatus = "PROCESSED"
)

// IsFinal возвращает true, если внутренний статус заказа является завершенным
// (PROCESSED или INVALID) и больше никогда не изменится в базе данных.
func (s OrderStatus) IsFinal() bool {
	return s == StatusProcessed || s == StatusInvalid
}

// IsUnprocessed возвращает true, если заказ находится в промежуточном состоянии
// (NEW или PROCESSING) и требует планового опроса воркером.
func (s OrderStatus) IsUnprocessed() bool {
	return s == StatusNew || s == StatusProcessing
}

// AccrualStatus описывает строго типизированные статусы ответов,
// приходящих от внешнего HTTP API системы начислений.
type AccrualStatus string

const (
	// AccrualRegistered означает, что заказ успешно зарегистрирован
	// во внешней системе, но расчет баллов по нему еще не начался.
	AccrualRegistered AccrualStatus = "REGISTERED"

	// AccrualProcessing означает, что внешняя система прямо сейчас
	// производит математический расчет баллов для этого заказа.
	AccrualProcessing AccrualStatus = "PROCESSING"

	// AccrualInvalid означает, что внешняя система завершила обработку
	// и признала данный заказ ошибочным или не подлежащим начислению.
	AccrualInvalid AccrualStatus = "INVALID"

	// AccrualProcessed означает, что внешняя система успешно рассчитала баллы,
	// и поле accrual в ответе содержит финальную сумму бонусов.
	AccrualProcessed AccrualStatus = "PROCESSED"
)

// IsFinal возвращает true, если статус ответа внешней системы начислений
// является окончательным (PROCESSED или INVALID).
func (a AccrualStatus) IsFinal() bool {
	return a == AccrualProcessed || a == AccrualInvalid
}

// Order представляет собой основную доменную модель заказа пользователя.
type Order struct {
	// ID хранит уникальный номер заказа, прошедший валидацию по алгоритму Луна.
	ID string `json:"number"`
	// UserID содержит UUID или уникальный идентификатор пользователя, загрузившего заказ.
	UserID string `json:"-"`
	// Status отражает текущую стадию обработки заказа в системе лояльности.
	Status OrderStatus `json:"status"`
	// Accrual хранит количество начисленных баллов лояльности за этот заказ.
	// Поле исключается из JSON-ответа (omitempty), если начислений еще нет.
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
	// UploadedAt фиксирует точную дату и время загрузки заказа пользователем.
	UploadedAt time.Time `json:"uploaded_at"`
}
