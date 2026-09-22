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

	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_Withdraw(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Зафиксированное правило: логируем в черную дыру
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)

	h := NewUserHandler(mockSvc, false, logger)

	userID := "user-123"
	validOrderID := "12345678903"

	t.Run("Success 200 OK", func(t *testing.T) {
		amount := decimal.NewFromFloat(100.50)
		jsonBody := `{"order":"` + validOrderID + `","sum":100.50}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Ожидаем, что хендлер вызовет метод Withdraw в сервисе
		mockSvc.EXPECT().
			Withdraw(gomock.Any(), userID, validOrderID, gomock.Any()).
			DoAndReturn(func(ctx context.Context, uid, oid string, sum decimal.Decimal) error {
				assert.True(t, sum.Equal(amount)) // Проверяем точность decimal
				return nil
			})

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code) // 200
	})

	t.Run("Failure 422 Unprocessable Entity (Invalid Luhn)", func(t *testing.T) {
		jsonBody := `{"order":"12345678904","sum":10.00}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Имитируем ошибку валидации алгоритма Луна из бизнес-логики
		mockSvc.EXPECT().
			Withdraw(gomock.Any(), userID, "12345678904", gomock.Any()).
			Return(service.ErrInvalidOrderNumber)

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code) // 422
	})

	t.Run("Failure 402 Payment Required (Insufficient Funds)", func(t *testing.T) {
		jsonBody := `{"order":"` + validOrderID + `","sum":9999.00}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Имитируем ошибку нехватки средств на балансе пользователя
		mockSvc.EXPECT().
			Withdraw(gomock.Any(), userID, validOrderID, gomock.Any()).
			Return(repository.ErrInsufficientFunds)

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusPaymentRequired, rec.Code) // 402
	})

	t.Run("Failure 400 Bad Request (Negative sum)", func(t *testing.T) {
		// Отрицательная сумма списания недопустима
		jsonBody := `{"order":"` + validOrderID + `","sum":-50.00}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure 400 Bad Request (Empty order)", func(t *testing.T) {
		jsonBody := `{"order":"   ","sum":10.00}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure 401 Unauthorized", func(t *testing.T) {
		jsonBody := `{"order":"` + validOrderID + `","sum":10.00}`

		// Контекст пустой, Middleware авторизации не сработал
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code) // 401
	})

	t.Run("Failure 500 Internal Server Error", func(t *testing.T) {
		jsonBody := `{"order":"` + validOrderID + `","sum":10.00}`

		ctx := context.WithValue(context.Background(), userIDContextKey, userID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Симулируем критический сбой инфраструктуры/базы данных
		mockSvc.EXPECT().
			Withdraw(gomock.Any(), userID, validOrderID, gomock.Any()).
			Return(errors.New("internal storage connection deadlock"))

		h.Withdraw(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code) // 500
	})
}

// это уникальный тест, доказывающий безопасность оптимизаций. если кто то в будущем забудет сбросить объект пула после обработки операции и вернет его в пул грязным с чужими данными то этот тест начнет падать при многокартном вызове
// go test -v -count=100 -run ^TestUserHandler_Withdraw_Concurrency$ ./internal/handler
func TestUserHandler_Withdraw_Concurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)
	h := NewUserHandler(mockSvc, false, logger)

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	// Мьютекс ТОЛЬКО для регистрации ожиданий внутри gomock
	var mockMu sync.Mutex

	for i := 0; i < workers; i++ {
		go func(wID int) {
			defer wg.Done()

			uID := fmt.Sprintf("user-%d", wID)
			oID := fmt.Sprintf("1234567890%d", wID%10)

			// Оставляем проверку с частичным JSON, чтобы проверить,
			// очищает ли Reset() данные при реальном последовательном переиспользовании пула.
			var body string
			if wID%2 == 0 {
				body = fmt.Sprintf(`{"order":"%s","sum":10.00}`, oID)
			} else {
				body = `{"sum":10.00}`
			}

			// Регистрируем ожидания только для честных ЧЕТНЫХ воркеров
			if wID%2 == 0 {
				mockMu.Lock()
				mockSvc.EXPECT().
					Withdraw(gomock.Any(), uID, oID, gomock.Any()).
					Return(nil).
					Times(1)
				mockMu.Unlock()
			}

			ctx := context.WithValue(context.Background(), userIDContextKey, uID)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/user/balance/withdraw", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()

			// ВЫЗЫВАЕМ ХЕНДЛЕР НАПРЯМУЮ.
			// Позволяем реальному sync.Pool внутри хендлера управлять памятью.
			h.Withdraw(rec, req)

			// Если в хендлере работает Reset(), нечетный воркер получит 400 Bad Request.
			// Если Reset() убрать, то при последовательном запуске горутины подхватят грязный кэш
			// пула и этот ассерт гарантированно упадет!
			if wID%2 != 0 && rec.Code == http.StatusOK {
				t.Errorf("SECURITY BREACH! Worker %d processed empty order but got 200 OK!", wID)
			}
		}(i)
	}

	wg.Wait()
}
