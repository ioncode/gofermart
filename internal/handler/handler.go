package handler

import (
	"context"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

//go:generate mockgen -source=handler.go -destination=../service/mocks/mock_loyalty.go -package=mocks

// LoyaltyService представляет собой центральный контракт бизнес-логики приложения,
// координирующий все ключевые процессы накопительной системы лояльности.
//
// Интерфейс декларирует полный набор методов, необходимых для обеспечения требований ТЗ:
//   - Управление жизненным циклом пользователей (регистрация, аутентификация, валидация сессий).
//   - Обработку номеров заказов с обязательной проверкой по алгоритму Луна.
//   - Учет накопительных счетов, контроль текущего баланса и фиксацию истории списаний.
//
// Реализация данного интерфейса выступает ядром доменного слоя (Service Layer)
// и изолирует HTTP-хендлеры от прямых низкоуровневых транзакций в базу данных PostgreSQL.
type LoyaltyService interface {
	// Register выполняет регистрацию нового аккаунта по паре логин/пароль.
	// После успешной регистрации должна происходить автоматическая аутентификация.
	Register(ctx context.Context, login string, password string) (token string, err error)

	// TokenTTL возвращает настроенную продолжительность жизни сессионного токена.
	TokenTTL() time.Duration

	// Authenticate проверяет учетные данные пользователя в системе.
	// Возвращает сгенерированный токен сессии при совпадении пары логин/пароль.
	Authenticate(ctx context.Context, login string, password string) (token string, err error)

	// UploadOrder принимает и валидирует новый номер заказа от авторизованного пользователя.
	// Номер проверяется по алгоритму Луна. Метод учитывает следующие сценарии:
	//   - Номер уже загружался текущим пользователем (обработка завершается со статусом 200).
	//   - Номер уже загружался другим пользователем (возвращается ошибка конфликта 409).
	//   - Номер имеет неверный формат (возвращается ошибка валидации 422).
	UploadOrder(ctx context.Context, userID string, orderID string) error

	// ValidateToken выполняет парсинг и криптографическую проверку JWT-токена сессии.
	// Возвращает UUID или строковый идентификатор пользователя при успешной валидации.
	ValidateToken(ctx context.Context, tokenString string) (userID string, err error)

	// GetOrders возвращает список всех загруженных пользователем номеров заказов.
	// По ТЗ выдача должна быть отсортирована от самых новых к самым старым.
	GetOrders(ctx context.Context, userID string) ([]domain.Order, error)

	// GetBalance возвращает текущий баланс баллов лояльности и общую сумму списаний.
	// Возвращает два значения: текущие доступные средства и сумму использованных баллов.
	GetBalance(ctx context.Context, userID string) (current decimal.Decimal, withdrawn decimal.Decimal, err error)

	// Withdraw фиксирует списание баллов в счет оплаты гипотетического нового заказа.
	// Метод прерывает транзакцию, если на счету недостаточно средств или номер заказа не прошел алгоритм Луна.
	Withdraw(ctx context.Context, userID string, orderID string, amount decimal.Decimal) error

	// GetWithdrawals возвращает полную историю успешных выводов и списаний баллов пользователя.
	// Выдачи в списке должны быть отсортированы по времени от самых новых к самым старым.
	GetWithdrawals(ctx context.Context, userID string) ([]domain.Withdrawal, error)
}

// UserHandler инкапсулирует слой HTTP-обработчиков для управления системой лояльности.
//
// Структура является единой точкой входа для маршрутов API и связывает входящие
// сетевые запросы с транзакционным сервисным слоем бизнес-логики. Данные о
// режиме окружения и логгер внедряются на этапе сборки приложения.
type UserHandler struct {
	// service содержит ссылку на реализацию интерфейса бизнес-логики.
	service LoyaltyService

	// isProd определяет флаг рабочей среды (true активирует Secure: true для кук сессии).
	isProd bool

	// logger обеспечивает структурированное логирование событий внутри хендлеров.
	logger ulog.Logger
}

// NewUserHandler является конструктором и возвращает настроенный указатель на UserHandler.
//
// Функция выполняет сборку зависимостей (Dependency Injection) для веб-интерфейса
// и автоматически обогащает контекст системного логгера метатегом компонента,
// избавляя внутренние эндпоинты от дублирования кода логирования.
func NewUserHandler(svc LoyaltyService, isProd bool, logger ulog.Logger) *UserHandler {
	return &UserHandler{
		service: svc,
		isProd:  isProd,
		logger:  logger.With(ulog.String("component", "HTTP handler")),
	}
}
