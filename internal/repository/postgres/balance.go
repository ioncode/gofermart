package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type BalanceRepository struct {
	db *pgxpool.Pool
}

// NewBalanceRepository создает новый экземпляр репозитория для работы с балансами пользователей
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewBalanceRepository(db *pgxpool.Pool) *BalanceRepository {
	return &BalanceRepository{db: db}
}

// GetUserBalance возвращает текущий доступный баланс пользователя и общую сумму его списаний за всё время.
//
// Метод считывает данные высокой точности NUMERIC напрямую в структуры decimal.Decimal,
// полностью исключая погрешности округления копеек на уровне бизнес-логики.
// Если пользователь не найден в системе, возвращает ошибку ErrUserNotFound.
func (r *BalanceRepository) GetUserBalance(ctx context.Context, userID string) (decimal.Decimal, decimal.Decimal, error) {
	// Запрос выбирает поля баланса и списаний для конкретного UUID пользователя
	query := `SELECT balance, withdrawn FROM users WHERE id = $1`

	var current decimal.Decimal
	var withdrawn decimal.Decimal

	// Выполняем точечное чтение одной строки
	err := r.db.QueryRow(ctx, query, userID).Scan(&current, &withdrawn)
	if err != nil {
		// Если СУБД вернула отсутствие строк, мапим ошибку на понятную для сервиса
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, decimal.Zero, repository.ErrUserNotFound
		}
		return decimal.Zero, decimal.Zero, fmt.Errorf("postgres: failed to scan user balance: %w", err)
	}

	return current, withdrawn, nil
}

func (r *BalanceRepository) WithdrawPoints(ctx context.Context, userID string, orderID string, amount decimal.Decimal) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: cannot start withdraw tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Обновляем баланс пользователя и сумму списаний
	userQuery := `UPDATE users SET balance = balance - $1, withdrawn = withdrawn + $1 WHERE id = $2`
	_, err = tx.Exec(ctx, userQuery, amount, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		// Код ошибки 23514 — Check Violation (наш check_balance_non_negative)
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return repository.ErrInsufficientFunds
		}
		return fmt.Errorf("postgres: failed to update user balance: %w", err)
	}

	// 2. Логируем факт списания в таблицу withdrawals
	withdrawQuery := `INSERT INTO withdrawals (user_id, order_id, amount) VALUES ($1, $2, $3)`
	_, err = tx.Exec(ctx, withdrawQuery, userID, orderID, amount)
	if err != nil {
		return fmt.Errorf("postgres: failed to record withdrawal: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: withdraw tx commit failed: %w", err)
	}

	return nil
}

// GetWithdrawals возвращает список списаний пользователя, отсортированных от новых к старым (DESC).
func (r *BalanceRepository) GetWithdrawals(ctx context.Context, userID string) ([]domain.Withdrawal, error) {
	// Исправлено на DESC согласно ТЗ: "от самых новых к самым старым"
	query := `
		SELECT order_id, amount, processed_at 
		FROM withdrawals 
		WHERE user_id = $1 
		ORDER BY processed_at DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query withdrawals: %w", err)
	}
	defer rows.Close()

	var withdrawals []domain.Withdrawal
	for rows.Next() {
		var w domain.Withdrawal
		if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, fmt.Errorf("postgres: failed to scan withdrawal row: %w", err)
		}
		withdrawals = append(withdrawals, w)
	}

	return withdrawals, nil
}
