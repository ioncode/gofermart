package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_UploadOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	userID := "user-123"
	validOrderID := "12345678903"   // Валидный номер по алгоритму Луна
	invalidOrderID := "12345678904" // Невалидный номер по алгоритму Луна

	t.Run("Success 202 Accepted (New order)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/orders", bytes.NewBufferString(validOrderID))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		mockSvc.EXPECT().
			UploadOrder(gomock.Any(), userID, validOrderID).
			Return(nil)

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusAccepted, rec.Code) // 202
	})

	t.Run("Success 200 OK (Already uploaded by same user)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/orders", bytes.NewBufferString(validOrderID))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		mockSvc.EXPECT().
			UploadOrder(gomock.Any(), userID, validOrderID).
			Return(service.ErrOrderUploadedBySameUser)

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code) // 200
	})

	t.Run("Conflict 409 (Already uploaded by another user)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/orders", bytes.NewBufferString(validOrderID))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		mockSvc.EXPECT().
			UploadOrder(gomock.Any(), userID, validOrderID).
			Return(service.ErrOrderUploadedByOtherUser)

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code) // 409
	})

	t.Run("Unprocessable Entity 422 (Luhn check failed)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		// Передаем невалидный номер заказа
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/orders", bytes.NewBufferString(invalidOrderID))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Ожидаем, что сервис даже не вызовется, так как валидация Луна упадет на уровне хендлера

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code) // 422
	})

	t.Run("Request Body Too Large 413", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		// Генерируем заведомо длинное тело запроса (больше 128 байт)
		largeBody := string(make([]byte, 210))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/orders", bytes.NewBufferString(largeBody))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code) // 413
	})

	t.Run("Unauthorized 401", func(t *testing.T) {
		// Контекст пустой, Middleware не записала userID
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/orders", bytes.NewBufferString(validOrderID))
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		h.UploadOrder(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code) // 401
	})
}

func TestUserHandler_GetOrders(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	userID := "user-123"

	t.Run("Success 200 OK with data and sorting check", func(t *testing.T) {
		now := time.Now()

		// Создаем тестовые данные.
		// По логике ТЗ, этот список репозиторий должен вернуть уже отсортированным (DESC).
		// Первым идет более новый заказ (создан только что), вторым — старый (создан час назад).
		mockOrders := []domain.Order{
			{
				ID:         "12345678903",
				UserID:     userID,
				Status:     domain.StatusProcessed,
				UploadedAt: now, // Новый заказ
			},
			{
				ID:         "79927398713",
				UserID:     userID,
				Status:     domain.StatusNew,
				UploadedAt: now.Add(-1 * time.Hour), // Старый заказ
			},
		}

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/orders", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Ожидаем вызов сервиса
		mockSvc.EXPECT().
			GetOrders(gomock.Any(), userID).
			Return(mockOrders, nil)

		// Вызываем хендлер
		h.GetOrders(rec, req)

		// Базовые проверки HTTP-ответа
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		// Десериализуем полученный JSON-массив
		var result []domain.Order
		err = json.Unmarshal(rec.Body.Bytes(), &result)
		require.NoError(t, err)

		// Проверяем количество элементов
		require.Len(t, result, 2)

		// СТРОГАЯ ПРОВЕРКА СОРТИРОВКИ (DESC):
		// Первый элемент в массиве обязан быть более свежим, чем второй.
		assert.True(t, result[0].UploadedAt.After(result[1].UploadedAt),
			"Заказы должны быть отсортированы от самых новых к самым старым (DESC)")

		// Проверяем корректность маппинга ID
		assert.Equal(t, "12345678903", result[0].ID)
		assert.Equal(t, "79927398713", result[1].ID)
	})

	t.Run("Success 204 No Content (Empty list)", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/orders", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		// Возвращаем пустой слайс
		mockSvc.EXPECT().
			GetOrders(gomock.Any(), userID).
			Return([]domain.Order{}, nil)

		h.GetOrders(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code) // 204
	})

	t.Run("Internal Server Error 500", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/orders", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()

		mockSvc.EXPECT().
			GetOrders(gomock.Any(), userID).
			Return(nil, errors.New("internal database issue"))

		h.GetOrders(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code) // 500
	})
}
