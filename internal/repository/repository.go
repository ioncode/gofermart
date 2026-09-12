package repository

import (
	"context"
	"errors"

	"github.com/ioncode/gofermart/internal/domain"
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
	// GetOrder ищет заказ по его номеру и возвращает модель заказа (ID, UserID, Status)
	GetOrder(ctx context.Context, orderID string) (domain.Order, error)
	// CreateOrder создает новую запись о заказе со статусом PROCESSING или NEW
	CreateOrder(ctx context.Context, orderID string, userID string, status string) error
}
