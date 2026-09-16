package repository

import (
	"context"
	"errors"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/shopspring/decimal"
)

// Доменные ошибки репозитория
var (
	ErrDuplicateLogin = errors.New("login already exists in database")
	ErrUserNotFound   = errors.New("user not found")
	ErrOrderNotFound  = errors.New("order not found")
)

// UserRepository описывает методы для работы с таблицей пользователей
type UserRepository interface {
	CreateUser(ctx context.Context, login, passwordHash string) (string, error)
	CheckUserExists(ctx context.Context, login string) (bool, error)
	GetPasswordHash(ctx context.Context, login string) (userID string, hash string, err error)
}

// OrderRepository описыввает методы для работы с хранилищем заказов
type OrderRepository interface {
	// GetOrder ищет заказ по его номеру и возвращает модель заказа (ID, UserID, Status)
	GetOrder(ctx context.Context, orderID string) (domain.Order, error)
	// CreateOrder создает новую запись о заказе со статусом PROCESSING или NEW
	CreateOrder(ctx context.Context, orderID string, userID string, status string) error
	// GetUnprocessedOrders вычитывает из базы список заказов в промежуточных статусах (NEW, PROCESSING)
	// для их последующей передачи во внешнюю систему расчета accrual.
	GetUnprocessedOrders(ctx context.Context) ([]domain.Order, error)
	// GetOrdersByUserID возвращает список всех заказов конкретного пользователя,
	// отсортированных по времени загрузки от самых старых к самым новым.
	GetOrdersByUserID(ctx context.Context, userID string) ([]domain.Order, error)
}

// BalanceRepository финансовый репозиторий
type BalanceRepository interface {
	// GetUserBalance возвращает данные о текущей сумме баллов лояльности, а также сумме использованных за весь период регистрации баллов.
	GetUserBalance(ctx context.Context, userID string) (decimal.Decimal, decimal.Decimal, error)
}

// OrderAccrualRepository фасадный репозиторий, инкапсулирующий транзакционную атомарную операцию
type OrderAccrualRepository interface {
	// UpdateOrderAndBalance переводит заказ в новый статус и начисляет баллы лояльности.
	// Метод должен выполняться внутри ACID-транзакции: обновление таблицы заказов и баланса пользователя.
	UpdateOrderAndBalance(ctx context.Context, orderID string, userID string, status string, accrual decimal.Decimal) error
}
