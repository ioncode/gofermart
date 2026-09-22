package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/repository/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"
)

// тушим логи, при разработке используем консольный вывод или os.std..
var logger = uzerolog.NewZerologAdapter(zerolog.New(io.Discard))

// Тестируем метод Register (Регистрация пользователя)
func TestLoyaltyService_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUserRepo := mocks.NewMockUserRepository(ctrl)
	mockOrderRepo := mocks.NewMockOrderRepository(ctrl)
	mockBalanceRepo := mocks.NewMockBalanceRepository(ctrl)

	secret := "test_secret_key_123"
	svc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, secret, 1*time.Hour, logger, nil)

	ctx := context.Background()
	login := "new_user"
	password := "secure_password"

	t.Run("Success registration", func(t *testing.T) {
		// 1. Сначала сервис проверяет, свободен ли логин. Возвращаем false (пользователь не существует)
		mockUserRepo.EXPECT().
			CheckUserExists(ctx, login).
			Return(false, nil)

		// 2. Затем сервис хэширует пароль и создает запись в БД
		mockUserRepo.EXPECT().
			CreateUser(ctx, login, gomock.Any()).
			Return("user-uuid-111", nil)

		token, err := svc.Register(ctx, login, password)

		assert.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("Conflict: login already exists (via CheckUserExists)", func(t *testing.T) {
		// Имитируем ситуацию, когда CheckUserExists сразу обнаружил дубликат и вернул true
		mockUserRepo.EXPECT().
			CheckUserExists(ctx, login).
			Return(true, nil)

		token, err := svc.Register(ctx, login, password)

		assert.Empty(t, token)
		assert.ErrorIs(t, err, ErrLoginConflict) // Сервис должен прервать выполнение и вернуть 409
	})

	t.Run("Conflict: login already exists (fallback via CreateUser)", func(t *testing.T) {
		// На случай, если CheckUserExists вернул false (например, из-за race condition между запросами),
		// но при вставке база данных выкинула ошибку уникальности индекса.
		mockUserRepo.EXPECT().
			CheckUserExists(ctx, login).
			Return(false, nil)

		mockUserRepo.EXPECT().
			CreateUser(ctx, login, gomock.Any()).
			Return("", repository.ErrDuplicateLogin)

		token, err := svc.Register(ctx, login, password)

		assert.Empty(t, token)
		assert.ErrorIs(t, err, ErrLoginConflict)
	})
}

// Тестируем метод Authenticate (Авторизация / Вход)
func TestLoyaltyService_Authenticate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUserRepo := mocks.NewMockUserRepository(ctrl)
	mockOrderRepo := mocks.NewMockOrderRepository(ctrl)
	mockBalanceRepo := mocks.NewMockBalanceRepository(ctrl)

	secret := "test_secret_key_123"
	svc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, secret, 1*time.Hour, logger, nil)

	ctx := context.Background()
	login := "existing_user"
	password := "correct_password"
	userID := "user-uuid-222"

	// Генерируем валидный bcrypt-хэш, который наш мок будет возвращать из «базы данных»
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)

	t.Run("Success authentication", func(t *testing.T) {
		// База данных успешно находит пользователя и отдает его ID и хэш пароля
		mockUserRepo.EXPECT().
			GetPasswordHash(ctx, login).
			Return(userID, string(hashedPassword), nil)

		token, err := svc.Authenticate(ctx, login, password)

		assert.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("Failure: user not found", func(t *testing.T) {
		// Репозиторий возвращает ошибку отсутствия записи в БД
		mockUserRepo.EXPECT().
			GetPasswordHash(ctx, "unknown_user").
			Return("", "", repository.ErrUserNotFound)

		token, err := svc.Authenticate(ctx, "unknown_user", password)

		assert.Empty(t, token)
		assert.ErrorIs(t, err, ErrInvalidCredentials) // Сервис должен скрыть детали и отдать общую ошибку 401
	})

	t.Run("Failure: incorrect password", func(t *testing.T) {
		// Пользователь есть, но пароль ввели неверный
		mockUserRepo.EXPECT().
			GetPasswordHash(ctx, login).
			Return(userID, string(hashedPassword), nil)

		token, err := svc.Authenticate(ctx, login, "wrong_password")

		assert.Empty(t, token)
		assert.ErrorIs(t, err, ErrInvalidCredentials) // Защита от перебора: ошибка та же самая
	})
}

// Тестируем метод UploadOrder (Загрузка заказа)
func TestLoyaltyService_UploadOrder(t *testing.T) {
	// Инициализируем контроллер для моков GoMock
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Создаем моки для каждого репозитория
	mockUserRepo := mocks.NewMockUserRepository(ctrl)
	mockOrderRepo := mocks.NewMockOrderRepository(ctrl)
	mockBalanceRepo := mocks.NewMockBalanceRepository(ctrl)

	// Инициализируем буферизованный канал для тестов
	orderChan := make(chan domain.Order, 1)

	// Создаем сервис, передавая туда наши моки
	svc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, "secret_key", 24*time.Hour, logger, orderChan)

	ctx := context.Background()
	userID := "user-123"
	orderID := "12345678903" // Валидный номер по алгоритму Луна

	t.Run("Success upload new order", func(t *testing.T) {
		// Ожидаем, что репозиторий успешно создаст новый заказ со статусом NEW
		mockOrderRepo.EXPECT().
			CreateOrder(ctx, orderID, userID, "NEW").
			Return(nil)

		err := svc.UploadOrder(ctx, userID, orderID)
		assert.NoError(t, err)

		// Проверяем, что заказ мгновенно улетел в канал воркера
		select {
		case o := <-orderChan:
			assert.Equal(t, orderID, o.ID)
			assert.Equal(t, userID, o.UserID)
			assert.Equal(t, domain.StatusNew, o.Status)
		default:
			t.Error("Заказ должен был быть отправлен в канал воркера, но канал пуст")
		}
	})

	t.Run("Conflict: order already uploaded by SAME user", func(t *testing.T) {
		existingOrder := domain.Order{ID: orderID, UserID: userID, Status: "NEW"}

		mockOrderRepo.EXPECT().
			CreateOrder(ctx, orderID, userID, "NEW").
			Return(repository.ErrOrderAlreadyExists)

		mockOrderRepo.EXPECT().
			GetOrder(ctx, orderID).
			Return(existingOrder, nil)

		err := svc.UploadOrder(ctx, userID, orderID)
		assert.ErrorIs(t, err, ErrOrderUploadedBySameUser)
	})

	t.Run("Conflict: order already uploaded by OTHER user", func(t *testing.T) {
		anotherUserID := "user-999"
		existingOrder := domain.Order{ID: orderID, UserID: anotherUserID, Status: "NEW"}

		mockOrderRepo.EXPECT().
			CreateOrder(ctx, orderID, userID, "NEW").
			Return(repository.ErrOrderAlreadyExists)

		mockOrderRepo.EXPECT().
			GetOrder(ctx, orderID).
			Return(existingOrder, nil)

		err := svc.UploadOrder(ctx, userID, orderID)
		assert.ErrorIs(t, err, ErrOrderUploadedByOtherUser)
	})
}

// Тестируем метод ValidateToken (Криптографическая проверка сессий)
func TestLoyaltyService_ValidateToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUserRepo := mocks.NewMockUserRepository(ctrl)
	mockOrderRepo := mocks.NewMockOrderRepository(ctrl)
	mockBalanceRepo := mocks.NewMockBalanceRepository(ctrl)

	secret := "my_super_secret_key_123"
	svc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, secret, 1*time.Hour, logger, nil)

	ctx := context.Background()
	testUserID := "4bc2b6f1-3310-4107-8e67-d86b5b5c9071"

	t.Run("Valid token passing", func(t *testing.T) {
		// Генерируем заведомо валидный токен с помощью встроенного метода автора
		validToken, err := svc.generateJWT(testUserID)
		require.NoError(t, err)

		// Проверяем токен
		parsedUserID, err := svc.ValidateToken(ctx, validToken)
		assert.NoError(t, err)
		assert.Equal(t, testUserID, parsedUserID)
	})

	t.Run("Expired token error", func(t *testing.T) {
		// Создаем сервис с протухшим временем жизни токена
		shortSvc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, secret, -1*time.Minute, logger, nil)
		expiredToken, err := shortSvc.generateJWT(testUserID)
		require.NoError(t, err)

		// Валидация должна вернуть ошибку парсинга (token is expired)
		_, err = svc.ValidateToken(ctx, expiredToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token parsing failed")
	})

	t.Run("Invalid signature token error", func(t *testing.T) {
		// Генерируем токен на ДРУГОМ секретном ключе
		alienSvc := NewLoyaltyService(mockUserRepo, mockOrderRepo, mockBalanceRepo, "wrong_secret_key_666", 1*time.Hour, logger, nil)
		alienToken, err := alienSvc.generateJWT(testUserID)
		require.NoError(t, err)

		// Валидация нашим основным сервисом должна завершиться ошибкой сигнатуры
		_, err = svc.ValidateToken(ctx, alienToken)
		assert.Error(t, err)
	})
}
