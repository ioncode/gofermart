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
