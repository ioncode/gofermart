package handler

import (
	"context"
)

type LoyaltyService interface {
	Register(ctx context.Context, login, password string) (token string, err error)
	// Login(ctx context.Context, login, password string) (token string, err error)
	// UploadOrder(ctx context.Context, userID string, orderID string) error
	// GetOrders(ctx context.Context, userID string) ([]byte, error)
	// GetBalance(ctx context.Context, userID string) ([]byte, error)
	// Withdraw(ctx context.Context, userID string, sum float64, orderID string) error
	// GetWithdrawals(ctx context.Context, userID string) ([]byte, error)
}

type UserHandler struct {
	service LoyaltyService
}

func NewUserHandler(svc LoyaltyService) *UserHandler {
	return &UserHandler{service: svc}
}
