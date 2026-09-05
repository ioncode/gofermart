package repository

import (
	"context"
	"errors"
)

// Доменные ошибки репозитория
var (
	ErrDuplicateLogin = errors.New("login already exists in database")
	ErrUserNotFound   = errors.New("user not found")
)

// UserRepository описывает методы для работы с таблицей пользователей
type UserRepository interface {
	CreateUser(ctx context.Context, login, passwordHash string) (string, error)
	CheckUserExists(ctx context.Context, login string) (bool, error)
	GetPasswordHash(ctx context.Context, login string) (userID string, hash string, err error)
}
