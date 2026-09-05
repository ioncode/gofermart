package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ioncode/gofermart/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrLoginConflict      = repository.ErrDuplicateLogin
	ErrInvalidCredentials = errors.New("invalid login or password")
)

type LoyaltyService struct {
	userRepo  repository.UserRepository
	jwtSecret []byte        // Секретный ключ для подписи токенов
	tokenTTL  time.Duration // Время жизни токена (например, 24 часа)
}

func NewLoyaltyService(repo repository.UserRepository, secret string, ttl time.Duration) *LoyaltyService {
	return &LoyaltyService{
		userRepo:  repo,
		jwtSecret: []byte(secret),
		tokenTTL:  ttl,
	}
}

// Claims — структура полезной нагрузки JWT
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

func (s *LoyaltyService) Register(ctx context.Context, login, password string) (string, error) {
	exists, err := s.userRepo.CheckUserExists(ctx, login)
	if err != nil {
		return "", fmt.Errorf("check user existence: %w", err)
	}
	if exists {
		return "", ErrLoginConflict
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	// Предположим, база данных создает UUID/ID и возвращает его
	userID, err := s.userRepo.CreateUser(ctx, login, string(hashedPassword))
	if err != nil {
		return "", fmt.Errorf("failed to create user: %w", err)
	}

	// 4. Генерируем настоящий JWT токен
	token, err := s.generateJWT(userID)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	return token, nil
}

// generateJWT создает подписанную строку JWT токена
func (s *LoyaltyService) generateJWT(userID string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		UserID: userID,
	}

	// Создаем токен с алгоритмом шифрования HS256
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Подписываем секретным ключом
	signedToken, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	return signedToken, nil
}
