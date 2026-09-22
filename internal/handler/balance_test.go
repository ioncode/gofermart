package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_GetBalance(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Инициализируем мок бизнес-логики и логгер-пустышку в io.Discard
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil)) // Направляет логи в "черную дыру"

	// Создаем хендлер, передавая туда мок-сервис
	h := NewUserHandler(mockSvc, false, logger)

	userID := "4bc2b6f1-3310-4107-8e67-d86b5b5c9071"

	t.Run("Success 200 OK", func(t *testing.T) {
		currentBalance := decimal.NewFromFloat(500.50)
		withdrawnAmount := decimal.NewFromFloat(42.00)

		// Настраиваем контекст с userID, имитируя работу AuthMiddleware
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)

		// Создаем тестовый HTTP-запрос
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/balance", nil)
		require.NoError(t, err)

		// Создаем ResponseRecorder для перехвата ответа сервера
		rec := httptest.NewRecorder()

		// Ожидаем, что хендлер вызовет бизнес-логику и получит баланс
		mockSvc.EXPECT().
			GetBalance(gomock.Any(), userID).
			Return(currentBalance, withdrawnAmount, nil)

		// Выполняем хендлер напрямую
		h.GetBalance(rec, req)

		// Проверяем HTTP статус-код и заголовок
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		// Десериализуем и проверяем структуру JSON-ответа
		var resp balanceDTO
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.True(t, resp.Current.Equal(currentBalance))
		assert.True(t, resp.Withdrawn.Equal(withdrawnAmount))
	})

	t.Run("Failure 401 Unauthorized (Context missing)", func(t *testing.T) {
		// Создаем контекст БЕЗ userIDContextKey
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/user/balance", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		h.GetBalance(rec, req)

		// Хендлер должен отсечь запрос с кодом 401
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Failure 405 Method Not Allowed", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)

		// Симулируем запрещенный POST запрос вместо GET
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		h.GetBalance(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})

	t.Run("Failure 500 Internal Server Error (DB fail)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/balance", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Имитируем сбой базы данных на уровне сервиса
		mockSvc.EXPECT().
			GetBalance(gomock.Any(), userID).
			Return(decimal.Zero, decimal.Zero, errors.New("database connection lost"))

		h.GetBalance(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

// BenchmarkUserHandler_GetBalance_Success измеряет скорость выполнения хендлера баланса
// в изоляции от СУБД, отсекая накладные расходы httptest.NewRequest с помощью таймера.
func BenchmarkUserHandler_GetBalance_Success(b *testing.B) {
	// Настраиваем логгер ulog на запись в никуда, чтобы исключить задержки дискового ввода-вывода
	logger := uzerolog.NewZerologAdapter(zerolog.New(io.Discard))

	// Переиспользуем наш легковесный стаб loyaltyServiceStub из login_test.go
	stubSvc := &loyaltyServiceStub{}
	h := NewUserHandler(stubSvc, false, logger)

	userID := "perf_balance_user"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Останавливаем счетчик времени бенчмарка на этапе сборки HTTP-окружения
		b.StopTimer()

		// Оборачиваем userIDContextKey
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/balance", nil)
		if err != nil {
			b.Fatalf("failed to create request: %v", err)
		}

		rec := httptest.NewRecorder()
		b.StartTimer()

		// Тестируем чистый вызов метода хендлера GetBalance
		h.GetBalance(rec, req)
	}
}

// BenchmarkStage4_WriteBalance_Optimized замеряет чистую скорость
// работы оптимизированного пула буферов при выдаче баланса.
func BenchmarkStage4_WriteBalance_Optimized(b *testing.B) {
	logger := uzerolog.NewZerologAdapter(zerolog.New(io.Discard))
	stubSvc := &loyaltyServiceStub{}
	h := NewUserHandler(stubSvc, false, logger)

	ctx := context.WithValue(context.Background(), userIDContextKey, "perf_balance_user")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/balance", nil)
	rec := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Вызываем наш обновленный метод хендлера, использующий WriteJSONOptimized
		h.GetBalance(rec, req)

		rec.Body.Reset()
	}
}
