package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/service/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestUserHandler_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))
	mockSvc := mocks.NewMockLoyaltyService(ctrl)

	h := NewUserHandler(mockSvc, false, logger)

	login := "new_user"
	password := "secure_password"
	tokenTTL := 24 * time.Hour

	t.Run("Success 200 OK", func(t *testing.T) {
		expectedToken := "new.valid.jwt.token"
		jsonBody := `{"login":"` + login + `","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/register", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Ожидаем вызовы бизнес-логики
		mockSvc.EXPECT().
			Register(gomock.Any(), login, password).
			Return(expectedToken, nil)

		// Вызывается ровно один раз благодаря оптимизации через локальную переменную ttl
		mockSvc.EXPECT().
			TokenTTL().
			Return(tokenTTL)

		h.Register(rec, req)

		// Проверяем статус-код
		assert.Equal(t, http.StatusOK, rec.Code)

		// Проверяем установку авторизационной куки
		resp := rec.Result()
		defer resp.Body.Close()
		cookies := resp.Cookies()
		require.Len(t, cookies, 1)

		authTokenCookie := cookies[0]
		assert.Equal(t, "auth_token", authTokenCookie.Name)
		assert.Equal(t, expectedToken, authTokenCookie.Value)
		assert.Equal(t, "/", authTokenCookie.Path)
		assert.True(t, authTokenCookie.HttpOnly)
		assert.Equal(t, int(tokenTTL.Seconds()), authTokenCookie.MaxAge)
	})

	t.Run("Failure 409 Conflict (Login already taken)", func(t *testing.T) {
		jsonBody := `{"login":"` + login + `","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/register", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Имитируем ошибку конфликта из бизнес-логики
		mockSvc.EXPECT().
			Register(gomock.Any(), login, password).
			Return("", service.ErrLoginConflict)

		h.Register(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code) // 409
	})

	t.Run("Failure 400 Bad Request (Empty credentials)", func(t *testing.T) {
		// Передаем пустую строку вместо логина
		jsonBody := `{"login":"   ","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/register", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Register(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure 400 Bad Request (Malformed JSON)", func(t *testing.T) {
		jsonBody := `{"login":"` + login + `", password: missing_quotes}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/register", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		h.Register(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure 500 Internal Server Error", func(t *testing.T) {
		jsonBody := `{"login":"` + login + `","password":"` + password + `"}`

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/user/register", bytes.NewBufferString(jsonBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()

		// Симулируем критический сбой инфраструктуры
		mockSvc.EXPECT().
			Register(gomock.Any(), login, password).
			Return("", errors.New("unexpected database crash"))

		h.Register(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code) // 500
	})
}
