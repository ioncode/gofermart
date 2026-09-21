package handler

import (
	"strings"
	"sync"

	"github.com/shopspring/decimal"
)

// RegisterRequest представляет DTO входящего запроса регистрации
type RegisterRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// registerRequestPool хранит указатели на структуры RegisterRequest.
// Использование указателей (*RegisterRequest) в sync.Pool гарантирует,
// что рантайм Go не будет упаковывать структуру в интерфейс any,
// полностью исключая escape to heap (выделение памяти в куче) на каждый запрос.
var registerRequestPool = sync.Pool{
	New: func() any {
		return new(RegisterRequest)
	},
}

// Reset полностью очищает поля структуры, подготавливая её к повторному использованию.
// Операция присваивания пустой структуры оптимизируется компилятором Go
// и выполняется на уровне регистров процессора абсолютно без аллокаций в куче.
func (r *RegisterRequest) Reset() {
	*r = RegisterRequest{}
}

// Sanitize выполняет «ленивую» обрезку пробелов у полей.
// Выделяет память под новые строки в куче ТОЛЬКО если пробелы реально есть.
// Логин приводится к нижнему регистру.
func (r *RegisterRequest) Sanitize() {
	if strings.HasPrefix(r.Login, " ") || strings.HasSuffix(r.Login, " ") {
		r.Login = strings.TrimSpace(r.Login)
	}
	if strings.HasPrefix(r.Password, " ") || strings.HasSuffix(r.Password, " ") {
		r.Password = strings.TrimSpace(r.Password)
	}

	r.Login = strings.ToLower(r.Login)
}

// Validate запускает ленивую очистку данных и проверяет обязательные поля на пустоту.
// Возвращает true, если запрос корректен.
func (r *RegisterRequest) Validate() bool {
	r.Sanitize()
	return r.Login != "" && r.Password != ""
}

// balanceDTO описывает транспортную структуру ответа API для отображения баланса.
// Используется в слое хендлеров для изоляции доменных моделей от деталей сериализации JSON.
type balanceDTO struct {
	// Current хранит текущую сумму доступных баллов лояльности на счету пользователя .
	Current decimal.Decimal `json:"current"`

	// Withdrawn хранит общую сумму баллов лояльности, списанных за всё время.
	Withdrawn decimal.Decimal `json:"withdrawn"`
}

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

// Sanitize выполняет ленивую очистку номера заказа.
func (r *WithdrawRequest) Sanitize() {
	if strings.HasPrefix(r.Order, " ") || strings.HasSuffix(r.Order, " ") {
		r.Order = strings.TrimSpace(r.Order)
	}
}

// Validate проверяет валидность финансовой транзакции.
func (r *WithdrawRequest) Validate() bool {
	r.Sanitize()
	return r.Order != "" && !r.Sum.IsNegative() && !r.Sum.IsZero()
}
