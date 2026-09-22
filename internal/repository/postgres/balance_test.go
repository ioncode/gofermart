package postgres_test

import (
	"context"
	"testing"

	"github.com/ioncode/gofermart/internal/repository"
	pgrepo "github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type BalanceRepoTestSuite struct {
	BasePostgresTestSuite
	repo *pgrepo.BalanceRepository
}

func (s *BalanceRepoTestSuite) SetupTest() {
	s.repo = pgrepo.NewBalanceRepository(s.Pool)
}

func (s *BalanceRepoTestSuite) TestWithdrawPoints() {
	ctx := context.Background()
	userID := "00000000-0000-0000-0000-000000000001"

	_, err := s.Pool.Exec(ctx,
		"INSERT INTO users (id, login, password_hash, balance, withdrawn) VALUES ($1, $2, $3, $4, $5)",
		userID, "loyalty_user", "hash", decimal.NewFromFloat(500.00), decimal.Zero,
	)
	s.Require().NoError(err)

	err = s.repo.WithdrawPoints(ctx, userID, "12345678901234", decimal.NewFromFloat(200.00))
	assert.NoError(s.T(), err)

	current, withdrawn, err := s.repo.GetUserBalance(ctx, userID)
	assert.NoError(s.T(), err)
	assert.True(s.T(), current.Equal(decimal.NewFromFloat(300.00)))
	assert.True(s.T(), withdrawn.Equal(decimal.NewFromFloat(200.00)))

	err = s.repo.WithdrawPoints(ctx, userID, "98765432109876", decimal.NewFromFloat(400.00))
	assert.ErrorIs(s.T(), err, repository.ErrInsufficientFunds)
}

func TestBalanceRepository(t *testing.T) {
	suite.Run(t, new(BalanceRepoTestSuite))
}
