package postgres_test

import (
	"context"
	"testing"

	"github.com/ioncode/gofermart/internal/domain"
	pgrepo "github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type OrderAccrualRepoTestSuite struct {
	BasePostgresTestSuite // Переиспользуем общую фикстуру базы данных и миграций
	repo                  *pgrepo.OrderAccrualRepository
}

func (s *OrderAccrualRepoTestSuite) SetupTest() {
	s.repo = pgrepo.NewOrderAccrualRepository(s.Pool)
}

// 1. Тест успешного начисления баллов (PROCESSED) со строгой проверкой констант домена
func (s *OrderAccrualRepoTestSuite) TestUpdateOrderAndBalance_SuccessAccrual() {
	ctx := context.Background()
	userID := "00000000-0000-0000-0000-000000000010"
	orderID := "12345678901234"

	// Подготавливаем фикстуру: создаем пользователя с нулевым балансом
	_, err := s.Pool.Exec(ctx,
		"INSERT INTO users (id, login, password_hash, balance, withdrawn) VALUES ($1, $2, $3, $4, $5)",
		userID, "accrual_user", "hash", decimal.Zero, decimal.Zero,
	)
	s.Require().NoError(err)

	// Создаем заказ, используя доменную константу StatusNew
	_, err = s.Pool.Exec(ctx,
		"INSERT INTO orders (id, user_id, status, accrual) VALUES ($1, $2, $3, $4)",
		orderID, userID, string(domain.StatusNew), decimal.Zero,
	)
	s.Require().NoError(err)

	// Атомарно переводим в статус PROCESSED и начисляем 150.55 баллов лояльности
	accrualAmount := decimal.NewFromFloat(150.55)
	err = s.repo.UpdateOrderAndBalance(ctx, orderID, userID, string(domain.StatusProcessed), accrualAmount)
	assert.NoError(s.T(), err)

	// Проверяем изменения в таблице заказов напрямую из базы
	var status string
	var dbAccrual decimal.Decimal
	err = s.Pool.QueryRow(ctx, "SELECT status, accrual FROM orders WHERE id = $1", orderID).Scan(&status, &dbAccrual)
	s.Require().NoError(err)

	// База данных должна вернуть строку, идентичную доменной константе
	assert.Equal(s.T(), string(domain.StatusProcessed), status)
	assert.True(s.T(), accrualAmount.Equal(dbAccrual))

	// Проверяем, что транзакция обновила баланс пользователя
	var balance decimal.Decimal
	err = s.Pool.QueryRow(ctx, "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance)
	s.Require().NoError(err)
	assert.True(s.T(), accrualAmount.Equal(balance))
}

// 2. Тест обработки невалидного заказа (INVALID) — баланс не должен увеличиваться
func (s *OrderAccrualRepoTestSuite) TestUpdateOrderAndBalance_InvalidOrderNoAccrual() {
	ctx := context.Background()
	userID := "00000000-0000-0000-0000-000000000020"
	orderID := "98765432109876"

	// Создаем пользователя с начальным балансом 10.00 баллов
	initialBalance := decimal.NewFromFloat(10.00)
	_, err := s.Pool.Exec(ctx,
		"INSERT INTO users (id, login, password_hash, balance, withdrawn) VALUES ($1, $2, $3, $4, $5)",
		userID, "invalid_order_user", "hash", initialBalance, decimal.Zero,
	)
	s.Require().NoError(err)

	// Создаем заказ со статусом NEW
	_, err = s.Pool.Exec(ctx,
		"INSERT INTO orders (id, user_id, status, accrual) VALUES ($1, $2, $3, $4)",
		orderID, userID, string(domain.StatusNew), decimal.Zero,
	)
	s.Require().NoError(err)

	// Переводим заказ в статус INVALID с нулевым начислением
	err = s.repo.UpdateOrderAndBalance(ctx, orderID, userID, string(domain.StatusInvalid), decimal.Zero)
	assert.NoError(s.T(), err)

	// Проверяем, что статус в БД стал INVALID
	var status string
	err = s.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderID).Scan(&status)
	s.Require().NoError(err)
	assert.Equal(s.T(), string(domain.StatusInvalid), status)

	// Баланс пользователя обязан остаться нетронутым (ровно 10.00)
	var balance decimal.Decimal
	err = s.Pool.QueryRow(ctx, "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance)
	s.Require().NoError(err)
	assert.True(s.T(), initialBalance.Equal(balance))
}

func TestOrderAccrualRepository(t *testing.T) {
	suite.Run(t, new(OrderAccrualRepoTestSuite))
}
