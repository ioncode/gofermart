package handler

import (
	"context"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

type LoyaltyService interface {
	Register(ctx context.Context, login string, password string) (token string, err error)
	TokenTTL() time.Duration
	Authenticate(ctx context.Context, login string, password string) (token string, err error)
	UploadOrder(ctx context.Context, userID string, orderID string) error
	ValidateToken(ctx context.Context, tokenString string) (userID string, err error)
	GetOrders(ctx context.Context, userID string) ([]domain.Order, error)
	GetBalance(ctx context.Context, userID string) (decimal.Decimal, decimal.Decimal, error)
	Withdraw(ctx context.Context, userID string, orderID string, amount decimal.Decimal) error
	GetWithdrawals(ctx context.Context, userID string) ([]domain.Withdrawal, error)
}

type UserHandler struct {
	service LoyaltyService
	isProd  bool // Флаг окружения (true активирует Secure: true для HTTPS в продакшене)
	logger  ulog.Logger
}

func NewUserHandler(svc LoyaltyService, isProd bool, logger ulog.Logger) *UserHandler {
	return &UserHandler{service: svc, isProd: isProd, logger: logger.With(ulog.String("component", "HTTP handler"))}
}
