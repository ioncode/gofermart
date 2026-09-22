package postgres_test

import (
	"context"
	"testing"

	"github.com/ioncode/gofermart/internal/domain"
	pgrepo "github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type OrderRepoTestSuite struct {
	BasePostgresTestSuite
	repo *pgrepo.OrderRepository
}

func (s *OrderRepoTestSuite) SetupTest() {
	s.repo = pgrepo.NewOrderRepository(s.Pool)
}

func (s *OrderRepoTestSuite) TestCreateAndGetOrder() {
	ctx := context.Background()
	userID := "00000000-0000-0000-0000-000000000002"
	orderID := "77777777777773"

	_, err := s.Pool.Exec(ctx, "INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)", userID, "order_user", "hash")
	s.Require().NoError(err)

	err = s.repo.CreateOrder(ctx, orderID, userID, string(domain.StatusNew))
	assert.NoError(s.T(), err)

	dbOrder, err := s.repo.GetOrder(ctx, orderID)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), orderID, dbOrder.ID)
	assert.Nil(s.T(), dbOrder.Accrual)
}

func TestOrderRepository(t *testing.T) {
	suite.Run(t, new(OrderRepoTestSuite))
}
