package handler

import (
	"context"
)

type LoyaltyService interface {
	Register(ctx context.Context, login string, password string) (token string, err error)
	Authenticate(ctx context.Context, login string, password string) (token string, err error)
	UploadOrder(ctx context.Context, userID string, orderID string) error
	ValidateToken(ctx context.Context, tokenString string) (userID string, err error)
	// GetOrders(ctx context.Context, userID string) ([]byte, error)
	// GetBalance(ctx context.Context, userID string) ([]byte, error)
	// Withdraw(ctx context.Context, userID string, sum float64, orderID string) error
	// GetWithdrawals(ctx context.Context, userID string) ([]byte, error)
}

type UserHandler struct {
	service LoyaltyService
	isProd  bool // Флаг окружения (true активирует Secure: true для HTTPS в продакшене)
}

func NewUserHandler(svc LoyaltyService, isProd bool) *UserHandler {
	return &UserHandler{service: svc}
}
