package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type OrderAccrualRepository struct {
	db *pgxpool.Pool
}

// NewOrderAccrualRepository создает новый фасадный репозиторий, инкапсулирующий ACID-транзакцию для начисления баллов.
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewOrderAccrualRepository(db *pgxpool.Pool) *OrderAccrualRepository {
	return &OrderAccrualRepository{db: db}
}

// UpdateOrderAccrual переводит заказ в новый статус и начисляет баллы лояльности в рамках ACID-транзакции.
func (r *OrderAccrualRepository) UpdateOrderAndBalance(ctx context.Context, orderID string, userID string, status string, accrual decimal.Decimal) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: cannot start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Обновляем статус и сумму начисления в таблице заказов
	orderQuery := `UPDATE orders SET status = $1, accrual = $2 WHERE id = $3`
	_, err = tx.Exec(ctx, orderQuery, status, accrual, orderID)
	if err != nil {
		return fmt.Errorf("postgres: tx update order failed: %w", err)
	}

	// 2. Начисляем баллы пользователю при положительной сумме
	if accrual.IsPositive() {
		userQuery := `UPDATE users SET balance = balance + $1 WHERE id = $2`
		_, err = tx.Exec(ctx, userQuery, accrual, userID)
		if err != nil {
			return fmt.Errorf("postgres: tx update user balance failed: %w", err)
		}
	}

	// 3. Фиксируем транзакцию
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: tx commit failed: %w", err)
	}

	return nil
}
