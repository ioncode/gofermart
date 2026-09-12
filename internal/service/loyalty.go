package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/ulog/v3"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrLoginConflict            = repository.ErrDuplicateLogin
	ErrInvalidCredentials       = errors.New("invalid login or password")
	ErrOrderUploadedBySameUser  = errors.New("order already uploaded by this user")
	ErrOrderUploadedByOtherUser = errors.New("order already uploaded by another user")
)

type LoyaltyService struct {
	userRepo  repository.UserRepository
	jwtSecret []byte        // Секретный ключ для подписи токенов
	tokenTTL  time.Duration // Время жизни токена (например, 24 часа)
	logger    ulog.Logger
}

func NewLoyaltyService(repo repository.UserRepository, secret string, ttl time.Duration, logger ulog.Logger) *LoyaltyService {
	logger.Debug("Инициализация сервиса Гофермарт")
	return &LoyaltyService{
		userRepo:  repo,
		jwtSecret: []byte(secret),
		tokenTTL:  ttl,
		logger:    logger,
	}
}

// Claims — структура полезной нагрузки JWT
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

func (s *LoyaltyService) Register(ctx context.Context, login string, password string) (string, error) {
	// СОЗДАНИЕ САБЛОГГЕРА ДЛЯ БИЗНЕС-ЛОГИКИ
	// Метод .With() привязывает login к контексту выполнения этой функции
	log := s.logger.With(ulog.String("login", login))
	log.Debug("Попытка регистрации нового пользователя")
	exists, err := s.userRepo.CheckUserExists(ctx, login)
	if err != nil {
		log.Error("Ошибка проверки существования пользователя", err)
		return "", fmt.Errorf("check user existence: %w", err)
	} else {
		log.Debug("Проверка сущестования пользователя успешно завершена")
	}
	if exists {
		log.Info("Пользователь с таким логином уже существует")
		return "", ErrLoginConflict
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("Не удалось сгенерировать хэш пароля", err)
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	// Предположим, база данных создает UUID/ID и возвращает его
	userID, err := s.userRepo.CreateUser(ctx, login, string(hashedPassword))
	if err != nil {
		log.Error("Не удалось создать пользователя", err)
		return "", fmt.Errorf("failed to create user: %w", err)
	} else {
		log.Debug("Пользователь успешно сохранен")
	}

	// 4. Генерируем настоящий JWT токен
	token, err := s.generateJWT(userID)
	if err != nil {
		log.Error("Не удалось сгенерировать JWT токен", err)
		return "", fmt.Errorf("failed to generate token: %w", err)
	} else {
		log.Debug("JWT токен успешно сгенерирован")
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

// Authenticate проверяет учетные данные пользователя через Bcrypt.
// В случае успеха генерирует и возвращает JWT-токен.
func (s *LoyaltyService) Authenticate(ctx context.Context, login string, password string) (string, error) {
	// Создаем контекстный логгер
	log := s.logger.With(ulog.String("login", login))
	log.Debug("Попытка аутентификации пользователя (Login)")

	// 1. Получаем ID и хэш пароля
	userID, passwordHash, err := s.userRepo.GetPasswordHash(ctx, login)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			log.Info("Пользователь с таким логином не найден")
			return "", ErrInvalidCredentials // Превращаем в безопасную ошибку 401
		}
		log.Error("Системная ошибка при поиске хэша пароля в БД", err)
		return "", fmt.Errorf("failed to get password hash: %w", err)
	}
	log.Debug("Пользователь найден, проверяем пароль...")

	// 2. Сравниваем сырой пароль с хэшем из PostgreSQL с помощью bcrypt
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			log.Info("Передан неверный пароль")
			return "", ErrInvalidCredentials // Снова возвращаем безопасную ошибку 401
		}
		log.Error("Критическая ошибка bcrypt верификации", err)
		return "", fmt.Errorf("bcrypt verification failed: %w", err)
	}
	log.Debug("Пароль успешно подтвержден")

	// 3. Генерируем JWT токен, переиспользуя приватный метод автора generateJWT
	token, err := s.generateJWT(userID)
	if err != nil {
		log.Error("Не удалось сгенерировать JWT сессию", err)
		return "", fmt.Errorf("failed to generate token for login: %w", err)
	}
	log.Debug("Аутентификация успешно завершена, JWT создан")

	return token, nil
}

func (s *LoyaltyService) UploadOrder(ctx context.Context, userID string, orderID string) error {
	// Создаем контекстный саблоггер
	log := s.logger.With(
		ulog.String("user_id", userID),
		ulog.String("order_id", orderID),
	)
	log.Debug("Попытка загрузки нового номера заказа")

	// 1. Проверяем существование заказа в базе данных через репозиторий
	existingOrder, err := s.userRepo.GetOrder(ctx, orderID)
	if err != nil {
		// Если это не ошибка отсутствия записи, значит произошел системный сбой БД
		if !errors.Is(err, repository.ErrOrderNotFound) {
			log.Error("Системная ошибка при проверке существования заказа в БД", err)
			return fmt.Errorf("failed to check order existence: %w", err)
		}
		// Если err == repository.ErrOrderNotFound, продолжаем выполнение: заказ абсолютно новый
	} else {
		// Заказ уже существует в системе. Проверяем, кто его владелец:
		if existingOrder.UserID == userID {
			log.Info("Заказ уже был загружен этим же пользователем ранее")
			return ErrOrderUploadedBySameUser
		}

		log.Info("Конфликт: заказ уже загружен другим пользователем")
		return ErrOrderUploadedByOtherUser
	}

	// 2. Сохраняем новый заказ в PostgreSQL со статусом "NEW"
	// Первоначальный баланс начисления равен 0, статус обработки — NEW
	err = s.userRepo.CreateOrder(ctx, orderID, userID, "NEW")
	if err != nil {
		log.Error("Не удалось сохранить новый заказ в базу данных", err)
		return fmt.Errorf("failed to save new order: %w", err)
	}
	log.Info("Новый заказ успешно сохранен в БД и принят в обработку")

	// 3. Асинхронный запуск планировщика расчета баллов лояльности
	// Мы запускаем фоновую горутину (или пишем задачу в канал воркера),
	// чтобы хендлер мгновенно вернул статус 202, не дожидаясь ответа от внешней системы.
	go s.fetchOrderAccrualAsync(orderID)

	return nil
}

// fetchOrderAccrualAsync — фоновый воркер, который будет опрашивать
// внешнюю систему расчета баллов лояльности ("черный ящик" из ТЗ)
func (s *LoyaltyService) fetchOrderAccrualAsync(orderID string) {
	// Здесь будет реализована логика фонового воркера (Worker Pool):
	// 1. HTTP-запрос к внешнему сервису рассчета баллов: GET /api/orders/{order_id}
	// 2. Обработка статусов ответа внешнего сервиса (PROCESSING, INVALID, PROCESSED)
	// 3. Обновление статуса заказа и баланса пользователя в БД внутри ACID транзакции
}

// ValidateToken проверяет подпись JWT токена и возвращает userID в случае успеха.
// Если токен протух, изменен или невалиден, возвращает ошибку.
func (s *LoyaltyService) ValidateToken(ctx context.Context, tokenString string) (string, error) {
	s.logger.Debug("Валидация JWT токена на уровне сервиса")

	claims := &Claims{}

	// Парсим и проверяем подпись с использованием s.jwtSecret
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		s.logger.Debug("Не удалось распарсить или верифицировать JWT токен")
		return "", fmt.Errorf("token parsing failed: %w", err)
	}

	if !token.Valid {
		s.logger.Debug("Предоставлен невалидный JWT токен")
		return "", errors.New("token is invalid")
	}

	if claims.UserID == "" {
		s.logger.Debug("В полезной нагрузке токена отсутствует user_id")
		return "", errors.New("token payload missing user id")
	}

	s.logger.Debug("Токен успешно верифицирован")
	return claims.UserID, nil
}
