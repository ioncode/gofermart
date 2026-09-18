package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestUserHandler_GetOrders_Concurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	// Количество параллельных горутин
	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	// Запускаем конкурентные запросы
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()

			// Формируем уникальные данные, чтобы проверить отсутствие пересечений в памяти
			userID := fmt.Sprintf("user-%d", workerID)
			orderID := fmt.Sprintf("order-id-%d", workerID)

			mockOrders := []domain.Order{
				{
					ID:         orderID,
					UserID:     userID,
					Status:     domain.StatusProcessed,
					UploadedAt: time.Now(),
				},
			}

			// Блокируем мьютекс на время регистрации ожидания в моке
			mockSvc.EXPECT().
				GetOrders(gomock.Any(), userID).
				Return(mockOrders, nil).
				Times(1)

			// Формируем контекст и http-запрос
			ctx := context.WithValue(context.Background(), userIDContextKey, userID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/user/orders", nil)
			if err != nil {
				t.Errorf("Worker %d: failed to create request: %v", workerID, err)
				return
			}

			rec := httptest.NewRecorder()

			// Вызываем хендлер
			h.GetOrders(rec, req)

			// Базовые проверки HTTP-ответа
			if rec.Code != http.StatusOK {
				t.Errorf("Worker %d: expected status 200, got %d", workerID, rec.Code)
				return
			}

			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Worker %d: expected Content-Type application/json, got %s", workerID, rec.Header().Get("Content-Type"))
				return
			}

			// Десериализуем и строго проверяем, что данные принадлежат ИМЕННО этому воркеру
			var result []domain.Order
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Errorf("Worker %d: failed to unmarshal JSON: %v. Body: %s", workerID, err, rec.Body.String())
				return
			}

			if len(result) != 1 {
				t.Errorf("Worker %d: expected 1 order, got %d", workerID, len(result))
				return
			}

			// Проверяем первый элемент массива на загрязнение данных (Data Contamination)
			if result[0].ID != orderID {
				t.Errorf("Worker %d: DATA CONTAMINATION DETECTED! Expected order %s, but got order %s",
					workerID, orderID, result[0].ID)
			}
		}(i)
	}

	// Ожидаем завершения работы всех горутин
	wg.Wait()
}
