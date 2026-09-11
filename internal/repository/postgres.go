package repository

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Встраиваем папку с миграциями прямо в бинарный файл приложения
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations запускает миграции базы данных перед стартом основных сервисов
func RunMigrations(databaseURL string) error {
	// Создаем источник данных для golang-migrate из встроенных файлов embed.FS
	sourceDriver, err := httpfs.New(http.FS(migrationsFS), "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("httpfs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrate instance: %w", err)
	}
	defer m.Close()

	// Применяем все доступные миграции "вверх"
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

// NewPostgresRepository создает новый экземпляр репозитория.
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// CreateUser сохраняет пользователя и возвращает сгенерированный базой данных UUID/ID
func (r *PostgresRepository) CreateUser(ctx context.Context, login, passwordHash string) (string, error) {
	query := `INSERT INTO users (login, password_hash) VALUES ($1, $2) RETURNING id`

	var userID string
	err := r.db.QueryRow(ctx, query, login, passwordHash).Scan(&userID)
	if err != nil {
		// Перехватываем ошибку уникальности PostgreSQL (код 23505 — unique_violation)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrDuplicateLogin
		}
		return "", fmt.Errorf("postgres insert user failed: %w", err)
	}

	return userID, nil
}

// CheckUserExists проверяет наличие логина в системе
func (r *PostgresRepository) CheckUserExists(ctx context.Context, login string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE login = $1)`

	var exists bool
	err := r.db.QueryRow(ctx, query, login).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("postgres check user existence failed: %w", err)
	}

	return exists, nil
}

// GetPasswordHash ищет пользователя по логину и возвращает его ID и хэш пароля для авторизации (Login)
func (r *PostgresRepository) GetPasswordHash(ctx context.Context, login string) (string, string, error) {
	query := `SELECT id, password_hash FROM users WHERE login = $1`

	var userID, hash string
	err := r.db.QueryRow(ctx, query, login).Scan(&userID, &hash)
	if err != nil {
		// Если ничего не найдено
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", "", err
		}
		return "", "", ErrUserNotFound
	}

	return userID, hash, nil
}
