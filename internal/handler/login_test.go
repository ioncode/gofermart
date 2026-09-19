package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_Login(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)

	h := NewUserHandler(mockSvc, false, logger)

	login := "test_user"
	password := "correct_password"
	tokenTTL := 2 * time.Hour

	t.Run("Success 200 OK", func(t *testing.T) {
		expectedToken := "valid.jwt.token"
		jsonBody := `{"login":"` + login + `","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/login", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Настраиваем мок сервиса на успешную аутентификацию и отдачу TTL
		mockSvc.EXPECT().
			Authenticate(gomock.Any(), login, password).
			Return(expectedToken, nil)

		mockSvc.EXPECT().
			TokenTTL().
			Return(tokenTTL)

		h.Login(rec, req)

		// Проверяем статус-код
		assert.Equal(t, http.StatusOK, rec.Code)

		// Проверяем, что кука установилась корректно
		resp := rec.Result()
		defer resp.Body.Close()
		cookies := resp.Cookies()
		require.Len(t, cookies, 1)

		authTokenCookie := cookies[0]
		assert.Equal(t, "auth_token", authTokenCookie.Name)
		assert.Equal(t, expectedToken, authTokenCookie.Value)
		assert.Equal(t, "/", authTokenCookie.Path)
		assert.True(t, authTokenCookie.HttpOnly)
	})

	t.Run("Failure 401 Unauthorized (Invalid credentials)", func(t *testing.T) {
		jsonBody := `{"login":"` + login + `","password":"wrong_password"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/login", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Имитируем ошибку неверных учетных данных из бизнес-логики
		mockSvc.EXPECT().
			Authenticate(gomock.Any(), login, "wrong_password").
			Return("", service.ErrInvalidCredentials)

		h.Login(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Failure 400 Bad Request (Empty fields)", func(t *testing.T) {
		// Передаем пустой пароль
		jsonBody := `{"login":"` + login + `","password":""}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/login", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Login(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Failure 400 Bad Request (Invalid JSON)", func(t *testing.T) {
		// Ломаем структуру JSON (пропущена закрывающая фигурная скобка)
		jsonBody := `{"login":"` + login + `","password":"` + password + `"`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/login", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Login(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Failure 500 Internal Server Error", func(t *testing.T) {
		jsonBody := `{"login":"` + login + `","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/login", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Симулируем падение базы данных или другую критическую ошибку на слое сервиса
		mockSvc.EXPECT().
			Authenticate(gomock.Any(), login, password).
			Return("", errors.New("unexpected internal db error"))

		h.Login(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

// BenchmarkUserHandler_Login_Success измеряет скорость выполнения и профиль выделения памяти
// при успешном сценарии аутентификации пользователя с использованием моков.
func BenchmarkUserHandler_Login_Success(b *testing.B) {
	// 1. Инициализируем контроллер моков, чтобы полностью исключить задержки базы данных и сети
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	// 2. Подготавливаем статические тестовые данные для JSON-тела запроса
	login := "perf_user"
	password := "perf_password"
	token := "mock.jwt.token"
	tokenTTL := 2 * time.Hour
	jsonPayload := []byte(`{"login":"` + login + `","password":"` + password + `"}`)

	// 3. Программируем поведение мока сервиса лояльности.
	// Метод AnyTimes() позволяет безопасно вызывать методы внутри горячего цикла бенчмарка.
	mockSvc.EXPECT().
		Authenticate(gomock.Any(), login, password).
		Return(token, nil).
		AnyTimes()

	mockSvc.EXPECT().
		TokenTTL().
		Return(tokenTTL).
		AnyTimes()

	// 4. Сбрасываем таймер перед стартом цикла, чтобы зафиксировать чистый результат
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Останавливаем таймер на время создания HTTP-запроса,
		// чтобы не учитывать накладные расходы и аллокации самого пакета httptest
		b.StopTimer()
		req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		b.StartTimer()

		// Вызываем целевой метод хендлера
		h.Login(rec, req)
	}
}

// сравним с вариантом на стабах вместо моков

// loyaltyServiceStub полностью реализует интерфейс LoyaltyService.
// Он работает со скоростью сырого процессора без аллокаций памяти в куче.
type loyaltyServiceStub struct{}

func (s *loyaltyServiceStub) Register(ctx context.Context, login, password string) (string, error) {
	return "", nil
}

func (s *loyaltyServiceStub) TokenTTL() time.Duration {
	return 2 * time.Hour
}

func (s *loyaltyServiceStub) Authenticate(ctx context.Context, login, password string) (string, error) {
	return "mock.jwt.token", nil
}

func (s *loyaltyServiceStub) UploadOrder(ctx context.Context, userID, orderID string) error {
	return nil
}

func (s *loyaltyServiceStub) ValidateToken(ctx context.Context, tokenString string) (string, error) {
	return "user-123", nil
}

// GetOrders возвращает тестовые данные в зависимости от переданного userID.
// Если передан маркер "perf_empty", метод возвращает пустой слайс для имитации ответа 204.
// В остальных случаях возвращается наполненный массив заказов для бенчмарка тяжелых данных.
func (s *loyaltyServiceStub) GetOrders(ctx context.Context, userID string) ([]domain.Order, error) {
	if userID == "perf_empty" {
		return []domain.Order{}, nil
	}

	// Инициализируем значение баллов лояльности для статуса PROCESSED
	accrualVal := decimal.NewFromFloat(500.23)

	return []domain.Order{
		{
			ID:         "12345678903",
			UserID:     userID,
			Status:     domain.StatusProcessed,
			Accrual:    &accrualVal, // Передаем указатель на структуру decimal
			UploadedAt: time.Now(),
		},
		{
			ID:         "79927398713",
			UserID:     userID,
			Status:     domain.StatusNew,
			Accrual:    nil, // Для статуса NEW начислений нет (будет опущен в JSON через omitempty)
			UploadedAt: time.Now().Add(-1 * time.Hour),
		},
	}, nil
}

func (s *loyaltyServiceStub) GetBalance(ctx context.Context, userID string) (decimal.Decimal, decimal.Decimal, error) {
	return decimal.Zero, decimal.Zero, nil
}

func (s *loyaltyServiceStub) Withdraw(ctx context.Context, userID, orderID string, amount decimal.Decimal) error {
	return nil
}

func (s *loyaltyServiceStub) GetWithdrawals(ctx context.Context, userID string) ([]domain.Withdrawal, error) {
	return nil, nil
}

// BenchmarkUserHandler_Login_Success замеряет чистую производительность хендлера аутентификации.
func BenchmarkUserHandler_Login_Success_with_stub(b *testing.B) {
	// Настраиваем логгер ulog на запись в io.Discard, чтобы исключить задержки дискового вывода
	logger := uzerolog.NewZerologAdapter(zerolog.New(io.Discard))

	// Инициализируем наш легкий стаб
	stubSvc := &loyaltyServiceStub{}
	h := NewUserHandler(stubSvc, false, logger)

	jsonPayload := []byte(`{"login":"perf_user","password":"perf_password"}`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Останавливаем счетчик времени накладных расходов пакета httptest
		b.StopTimer()
		req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		b.StartTimer()

		// Тестируем чистый вызов метода хендлера
		h.Login(rec, req)
	}
}
