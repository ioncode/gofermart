package router_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/router/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestServerRouterAndMiddleware_WithMocks(t *testing.T) {
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))

	// 1. Тест MaxBytesMiddleware: отправляем больше лимита
	t.Run("MaxBytesMiddleware_TooLarge", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		// Настройка для этапа инициализации chi.Router
		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHandler.Register(w, r)
		}))

		mockHandler.On("Register", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(0).(http.ResponseWriter)
			r := args.Get(1).(*http.Request)

			_, err := io.Copy(io.Discard, r.Body)
			if err != nil && err.Error() == "http: request body too large" {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
		})

		srv := router.NewServer("", mockHandler, logger)

		largeBody := make([]byte, handler.MaxBodySize+1)
		req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})

	// 2. Тест chi.middleware.AllowContentType JSON validation
	t.Run("AllowContentType_InvalidMIME", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		// Удовлетворяем обязательный вызов chi.Chain при инициализации NewServer
		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

		srv := router.NewServer(":8080", mockHandler, logger)

		req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	})

	// 3. Тест AuthMiddleware: закрытый эндпоинт без токена авторизации
	t.Run("AuthMiddleware_Unauthorized", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		// Программируем боевой отказ в доступе внутри middleware
		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))

		srv := router.NewServer(":8080", mockHandler, logger)

		req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	// 4. Тест Успешного прохождения сквозь всю цепочку авторизации к эндпоинту
	t.Run("Success_Authorized_Order_Upload", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		// Пропускаем запрос дальше по цепочке к методу
		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHandler.UploadOrder(w, r)
		}))

		mockHandler.On("UploadOrder", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(0).(http.ResponseWriter)
			w.WriteHeader(http.StatusOK)
		})

		srv := router.NewServer(":8080", mockHandler, logger)

		req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("12345678901234")))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Authorization", "Bearer valid_mock_token")
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Gzip_CompressResponse", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHandler.Register(w, r)
		}))

		mockHandler.On("Register", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(0).(http.ResponseWriter)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

		srv := router.NewServer(":8080", mockHandler, logger)

		req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader([]byte(`{"login":"test","password":"pwd"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

		reader, err := gzip.NewReader(rec.Body)
		assert.NoError(t, err)
		defer reader.Close()

		unzippedBody, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"status":"ok"}`, string(unzippedBody))

	})

	t.Run("Gzip_DecompressRequest", func(t *testing.T) {
		mockHandler := new(mocks.MockServerHandler)

		mockHandler.On("AuthMiddleware", mock.Anything).Return(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHandler.Register(w, r)
		}))

		mockHandler.On("Register", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(0).(http.ResponseWriter)
			r := args.Get(1).(*http.Request)
			body, _ := io.ReadAll(r.Body)
			if string(body) == `{"login":"test","password":"pwd"}` {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusBadRequest)
			}
		})

		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"login":"test","password":"pwd"}`))
		_ = zw.Close()

		srv := router.NewServer(":8080", mockHandler, logger)

		req := httptest.NewRequest(http.MethodPost, "/api/user/register", &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		rec := httptest.NewRecorder()

		srv.GetHandler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

}
