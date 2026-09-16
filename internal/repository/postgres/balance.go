package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ioncode/gofermart/internal/repository"
	"github.com/jackc/pgx/v5"
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
