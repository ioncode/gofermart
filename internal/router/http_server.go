package router

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ioncode/gofermart/internal/handler"
)

type Server struct {
	httpServer *http.Server
}

// NewServer собирает http.Server, подключает middleware и настраивает маршруты
func NewServer(port string, userHandler *handler.UserHandler) *Server {
	r := chi.NewRouter()

	// Настройка стандартных Middleware для логирования и стабильности
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Группировка эндпоинтов согласно ТЗ накопительной системы
	r.Route("/api/user", func(r chi.Router) {
		// Публичные эндпоинты (Аутентификация и регистрация)
		r.Post("/register", userHandler.Register)
		// r.Post("/login", userHandler.Login)

		// // Защищенные эндпоинты (доступны только авторизованным пользователям)
		// r.Group(func(r chi.Router) {
		// 	// В будущем здесь подключается middleware проверки JWT/Cookie:
		// 	// r.Use(authMiddleware)

		// 	r.Post("/orders", userHandler.UploadOrder)
		// 	r.Get("/orders", userHandler.GetOrders)

		// 	r.Get("/balance", userHandler.GetBalance)
		// 	r.Post("/balance/withdraw", userHandler.Withdraw)

		// 	r.Get("/withdrawals", userHandler.GetWithdrawals)
		// })
	})

	return &Server{
		httpServer: &http.Server{
			Addr:    ":" + port,
			Handler: r,
		},
	}
}

// Start запускает сервер в текущей горутине
func (s *Server) Start() {
	log.Printf("[Router] Gophermart API сервер запущен на %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("[Router] Критическая ошибка при работе сервера: %v", err)
	}
}

// Stop выполняет Graceful Shutdown с использованием переданного контекста таймаута
func (s *Server) Stop(ctx context.Context) error {
	log.Println("[Router] Завершение работы HTTP-сервера и обработка оставшихся запросов...")
	return s.httpServer.Shutdown(ctx)
}
