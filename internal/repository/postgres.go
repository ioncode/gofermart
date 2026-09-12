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
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/jackc/pgx/v5"
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

// GetOrder ищет заказ по его уникальному номеру с использованием pgxpool
func (r *PostgresRepository) GetOrder(ctx context.Context, orderID string) (domain.Order, error) {
	query := `SELECT id, user_id, status, accrual, uploaded_at FROM orders WHERE id = $1`

	var o domain.Order
	// В pgx можно сканировать NUMERIC напрямую в float64, но если там NULL,
	// лучше использовать указатель *float64, чтобы избежать падения
	var accrual *float64

	// ИСПРАВЛЕНО: QueryRowContext -> QueryRow
	err := r.db.QueryRow(ctx, query, orderID).Scan(
		&o.ID,
		&o.UserID,
		&o.Status,
		&accrual,
		&o.UploadedAt,
	)

	if err != nil {
		// ИСПРАВЛЕНО: Проверка на отсутствие строк в pgx делается через pgx.ErrNoRows
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, ErrOrderNotFound
		}
		return domain.Order{}, fmt.Errorf("postgres: get order execution failed: %w", err)
	}

	if accrual != nil {
		o.Accrual = *accrual
	}

	return o, nil
}

// CreateOrder сохраняет новый заказ в базу данных со статусом NEW
func (r *PostgresRepository) CreateOrder(ctx context.Context, orderID string, userID string, status string) error {
	query := `INSERT INTO orders (id, user_id, status, accrual) VALUES ($1, $2, $3, 0.00)`

	// ИСПРАВЛЕНО: ExecContext -> Exec
	_, err := r.db.Exec(ctx, query, orderID, userID, status)
	if err != nil {
		return fmt.Errorf("postgres: failed to insert new order: %w", err)
	}

	return nil
}

// UpdateOrderAccrual обновляет статус заказа и увеличивает баланс пользователя внутри транзакции pgx
func (r *PostgresRepository) UpdateOrderAccrual(ctx context.Context, orderID string, userID string, status string, accrual float64) error {
	// 1. Стартуем транзакцию через pgxpool
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: cannot start transaction: %w", err)
	}
	// Гарантируем откат в случае ошибки
	defer tx.Rollback(ctx)

	// 2. Обновляем статус и сумму начисления в таблице заказов
	orderQuery := `UPDATE orders SET status = $1, accrual = $2 WHERE id = $3`
	_, err = tx.Exec(ctx, orderQuery, status, accrual, orderID)
	if err != nil {
		return fmt.Errorf("postgres: tx update order failed: %w", err)
	}

	// 3. Если баллы лояльности больше нуля, прибавляем их к балансу пользователя
	if accrual > 0 {
		userQuery := `UPDATE users SET balance = balance + $1 WHERE id = $2`
		_, err = tx.Exec(ctx, userQuery, accrual, userID)
		if err != nil {
			return fmt.Errorf("postgres: tx update user balance failed: %w", err)
		}
	}

	// 4. Фиксируем изменения в базе данных
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: tx commit failed: %w", err)
	}

	return nil
}
