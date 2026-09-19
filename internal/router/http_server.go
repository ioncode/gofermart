package router

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ioncode/gofermart/internal/handler"
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
	r.Use(middleware.Timeout(60 * time.Second))

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

// MaxBytesMiddleware ограничивает размер тела запроса фиксированным лимитом.
// При превышении лимита http.MaxBytesReader возвращает "http: request body too large"
// и аппаратно разрывает TCP-соединение на уровне ядра ОС при r.Body.Close().
func MaxBytesMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Оборачиваем оригинальный r.Body в лимитер.
			// Объект MaxBytesReader выделится в куче ОДИН РАЗ на уровне роутера,
			// что полностью изолирует наш горячий helper.go от аллокаций.
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// GetHandler возвращает настроенный роутер (http.Handler) для использования в интеграционных тестах.
func (s *Server) GetHandler() http.Handler {
	return s.httpServer.Handler
}
