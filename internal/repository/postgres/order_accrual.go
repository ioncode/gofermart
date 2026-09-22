package postgres

import (
	"context"
	"fmt"

	"github.com/ioncode/gofermart/internal/repository"
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
// для предотвращения взаимных блокировок с транзакцией списания порядок блокировки users -> orders
func (r *OrderAccrualRepository) UpdateOrderAndBalance(ctx context.Context, orderID string, userID string, status string, accrual decimal.Decimal) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: cannot start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if accrual.IsPositive() {
		userQuery := `UPDATE users SET balance = balance + $1 WHERE id = $2`
		res, err := tx.Exec(ctx, userQuery, accrual, userID)
		if err != nil {
			return fmt.Errorf("postgres: tx update user balance failed: %w", err)
		}

		if res.RowsAffected() == 0 {
			return fmt.Errorf("postgres: user %s not found: %w", userID, repository.ErrUserNotFound)
		}
	}

	//обновить пользователь может только свой заказ в нефинальном статусе
	orderQuery := `UPDATE orders SET status = $1, accrual = $2 WHERE id = $3 AND user_id = $4 AND status NOT IN ('PROCESSED', 'INVALID')`
	res, err := tx.Exec(ctx, orderQuery, status, accrual, orderID, userID)
	if err != nil {
		return fmt.Errorf("postgres: tx update order failed: %w", err)
	}

	if res.RowsAffected() == 0 {
		return fmt.Errorf("postgres: order %s for user %s already processed or not found: %w", orderID, userID, repository.ErrOrderNotFound)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: tx commit failed: %w", err)
	}
	return nil
}
