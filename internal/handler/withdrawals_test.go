package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_GetWithdrawals(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Зафиксированное правило: логируем в черную дыру
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	userID := "user-123"

	t.Run("Success 200 OK with data and sorting check", func(t *testing.T) {
		now := time.Now()

		// Подготавливаем тестовые данные. Согласно ТЗ, список возвращается отсортированным (DESC).
		// Первый элемент — более новое списание, второй — старое (сделано час назад).
		mockWithdrawals := []domain.Withdrawal{
			{
				Order:       "12345678903",
				Sum:         decimal.NewFromFloat(100.50),
				ProcessedAt: now,
			},
			{
				Order:       "79927398713",
				Sum:         decimal.NewFromFloat(50.00),
				ProcessedAt: now.Add(-1 * time.Hour),
			},
		}

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/withdrawals", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Ожидаем вызов бизнес-логики
		mockSvc.EXPECT().
			GetWithdrawals(gomock.Any(), userID).
			Return(mockWithdrawals, nil)

		h.GetWithdrawals(rec, req)

		// Базовые проверки ответа
		assert.Equal(t, http.StatusOK, rec.Code) // 200
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		// Десериализуем ответ
		var result []domain.Withdrawal
		err = json.Unmarshal(rec.Body.Bytes(), &result)
		require.NoError(t, err)
		require.Len(t, result, 2)

		// СТРОГАЯ ПРОВЕРКА СОРТИРОВКИ (DESC):
		// Первый элемент в массиве обязан быть более свежим, чем второй.
		assert.True(t, result[0].ProcessedAt.After(result[1].ProcessedAt),
			"Факты выводов в выдаче должны быть отсортированы по времени от самых новых к самым старым (DESC)")

		// Проверяем корректность маппинга данных
		assert.Equal(t, "12345678903", result[0].Order)
		assert.True(t, result[0].Sum.Equal(decimal.NewFromFloat(100.50)))
		assert.Equal(t, "79927398713", result[1].Order)
		assert.True(t, result[1].Sum.Equal(decimal.NewFromFloat(50.00)))
	})

	t.Run("Success 204 No Content (Empty list)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/withdrawals", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Возвращаем пустой слайс (у пользователя еще нет списаний)
		mockSvc.EXPECT().
			GetWithdrawals(gomock.Any(), userID).
			Return([]domain.Withdrawal{}, nil)

		h.GetWithdrawals(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code) // 204
	})

	t.Run("Failure 401 Unauthorized", func(t *testing.T) {
		// Запрос без userID в контексте (Middleware не сработал или токен сломан)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/user/withdrawals", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		h.GetWithdrawals(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code) // 401
	})

	t.Run("Failure 500 Internal Server Error", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/withdrawals", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Симулируем критический сбой инфраструктуры/базы данных
		mockSvc.EXPECT().
			GetWithdrawals(gomock.Any(), userID).
			Return(nil, errors.New("internal storage connection timeout"))

		h.GetWithdrawals(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code) // 500
	})
}
