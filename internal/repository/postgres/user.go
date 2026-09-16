package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ioncode/gofermart/internal/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

// NewUserRepository создает новый экземпляр репозитория для работы с таблицей пользователей
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

// CreateUser сохраняет пользователя и возвращает сгенерированный базой данных UUID/ID
func (r *UserRepository) CreateUser(ctx context.Context, login, passwordHash string) (string, error) {
	query := `INSERT INTO users (login, password_hash) VALUES ($1, $2) RETURNING id`

	var userID string
	err := r.db.QueryRow(ctx, query, login, passwordHash).Scan(&userID)
	if err != nil {
		// Перехватываем ошибку уникальности PostgreSQL (код 23505 — unique_violation)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", repository.ErrDuplicateLogin
		}
		return "", fmt.Errorf("postgres insert user failed: %w", err)
	}

	return userID, nil
}

// CheckUserExists проверяет наличие логина в системе
func (r *UserRepository) CheckUserExists(ctx context.Context, login string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE login = $1)`

	var exists bool
	err := r.db.QueryRow(ctx, query, login).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("postgres check user existence failed: %w", err)
	}

	return exists, nil
}

// GetPasswordHash ищет пользователя по логину и возвращает его ID и хэш пароля для авторизации (Login)
func (r *UserRepository) GetPasswordHash(ctx context.Context, login string) (string, string, error) {
	query := `SELECT id, password_hash FROM users WHERE login = $1`

	var userID, hash string
	err := r.db.QueryRow(ctx, query, login).Scan(&userID, &hash)
	if err != nil {
		// Если ничего не найдено
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", "", err
		}
		return "", "", repository.ErrUserNotFound
	}

	return userID, hash, nil
}
