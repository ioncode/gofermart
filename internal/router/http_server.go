package router

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/pkg/gzip"
	"github.com/ioncode/ulog/v3"
)

// Server инкапсулирует конфигурацию и управление жизненным циклом HTTP-сервера.
type Server struct {
	httpServer *http.Server
	logger     ulog.Logger
}

// NewServer создает и настраивает новый экземпляр HTTP-сервера.
//
// В качестве обработчика бизнес-логики принимает ServerHandler, который с помощью
// композиции объединяет контракты аутентификации, управления заказами и финансов.
// Метод инициализирует chi.Router, подключает базовые и кастомные Middleware,
// а также разворачивает дерево эндпоинтов системы лояльности.
func NewServer(addr string, srvHandler ServerHandler, logger ulog.Logger) *Server {
	logger = logger.With(ulog.String("component", "HTTP server"))
	r := chi.NewRouter()

	// Настройка стандартных Middleware для логирования и стабильности
	r.Use(ulog.TraceIDMiddleware)
	r.Use(ulog.RecoveryMiddleware(logger))
	r.Use(ulog.LoggingMiddleware(logger))
	r.Use(gzip.Middleware)

	// DoS-защита для чтения через helper
	r.Use(MaxBytesMiddleware(handler.MaxBodySize))

	// Группировка эндпоинтов согласно ТЗ накопительной системы
	r.Route("/api/user", func(r chi.Router) {
		// Публичные эндпоинты (Аутентификация и регистрация)
		r.With(middleware.AllowContentType("application/json")).Post("/register", srvHandler.Register)
		r.With(middleware.AllowContentType("application/json")).Post("/login", srvHandler.Login)

		// Защищенные эндпоинты (доступны только авторизованным пользователям)
		r.Group(func(r chi.Router) {
			r.Use(srvHandler.AuthMiddleware)

			r.With(middleware.AllowContentType("text/plain")).Post("/orders", srvHandler.UploadOrder)
			r.Get("/orders", srvHandler.GetOrders)

			r.Get("/balance", srvHandler.GetBalance)
			r.With(middleware.AllowContentType("application/json")).Post("/balance/withdraw", srvHandler.Withdraw)

			r.Get("/withdrawals", srvHandler.GetWithdrawals)
		})
	})

	return &Server{
		logger: logger,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: r,
			// =========================================================================
			// НАСТРОЙКА СЕТЕВЫХ ТАЙМАУТОВ ДЛЯ ЗАЩИТЫ ОТ МЕДЛЕННЫХ АТАК (SLOWLORIS DOS)
			// =========================================================================
			ReadTimeout:       5 * time.Second,   // Максимальное время на чтение всего запроса (включая заголовки и тело)
			WriteTimeout:      10 * time.Second,  // Максимальное время на запись ответа клиенту в сетевой сокет
			IdleTimeout:       120 * time.Second, // Время удержания Keep-Alive соединения в режиме ожидания нового запроса
			ReadHeaderTimeout: 2 * time.Second,   // Жесткий лимит конкретно на вычитку HTTP-заголовков (первая линия обороны)
			// =========================================================================
		},
	}
}

// Start запускает сервер и возвращает ошибку.
func (s *Server) Start() error {
	s.logger.Info("Gophermart API сервер запущен", ulog.String("Адрес сервера", s.httpServer.Addr))

	if err := s.httpServer.ListenAndServe(); err != nil {
		// Ошибку штатного закрытия сервера (http.ErrServerClosed) мы не считаем сбоем,
		// возвращаем nil, чтобы errgroup не паниковала при graceful shutdown.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		s.logger.Error("Критическая ошибка при работе сервера", err)
		return err
	}

	return nil
}

// Stop выполняет Graceful Shutdown с использованием переданного контекста таймаута
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Завершение работы веб-сервера и обработка оставшихся запросов...")
	return s.httpServer.Shutdown(ctx)
}

// MaxBytesMiddleware ограничивает размер тела запроса фиксированным лимитом.
// При превышении лимита http.MaxBytesReader возвращает "http: request body too large"
// и аппаратно разрывает TCP-соединение на уровне ядра ОС при r.Body.Close().
func MaxBytesMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetHandler возвращает настроенный роутер (http.Handler) для использования в интеграционных тестах.
func (s *Server) GetHandler() http.Handler {
	return s.httpServer.Handler
}
