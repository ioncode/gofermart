package router

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/ulog/v3"
)

type Server struct {
	httpServer *http.Server
	logger     ulog.Logger
}

// NewServer собирает http.Server, подключает middleware и настраивает маршруты
func NewServer(addr string, userHandler *handler.UserHandler, logger ulog.Logger) *Server {
	logger = logger.With(ulog.String("component", "HTTP server"))
	r := chi.NewRouter()

	// Настройка стандартных Middleware для логирования и стабильности
	r.Use(ulog.TraceIDMiddleware)
	r.Use(ulog.RecoveryMiddleware(logger))
	r.Use(ulog.LoggingMiddleware(logger))

	// Группировка эндпоинтов согласно ТЗ накопительной системы
	r.Route("/api/user", func(r chi.Router) {
		// Публичные эндпоинты (Аутентификация и регистрация)
		r.With(middleware.AllowContentType("application/json")).Post("/register", userHandler.Register)
		r.With(middleware.AllowContentType("application/json")).Post("/login", userHandler.Login)

		// Защищенные эндпоинты (доступны только авторизованным пользователям)
		r.Group(func(r chi.Router) {
			r.Use(userHandler.AuthMiddleware)

			r.Post("/orders", userHandler.UploadOrder)
			// r.Get("/orders", userHandler.GetOrders)

			// r.Get("/balance", userHandler.GetBalance)
			// r.Post("/balance/withdraw", userHandler.Withdraw)

			// r.Get("/withdrawals", userHandler.GetWithdrawals)
		})
	})

	return &Server{
		logger: logger,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: r,
		},
	}
}

// Start запускает сервер в текущей горутине
func (s *Server) Start() {
	s.logger.Info("Gophermart API сервер запущен", ulog.String("Адрес сервера", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("Критическая ошибка при работе сервера", err)
		os.Exit(1)
	}
}

// Stop выполняет Graceful Shutdown с использованием переданного контекста таймаута
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Завершение и обработка оставшихся запросов...")
	return s.httpServer.Shutdown(ctx)
}
