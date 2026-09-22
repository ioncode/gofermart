// Пакет main является главной точкой входа в накопительную систему лояльности «Гофермарт».
//
// Файл выполняет глобальную инициализацию инфраструктурных компонентов приложения:
//   - Парсинг и валидацию конфигурационных флагов и переменных окружения.
//   - Настройку структурированного логирования на базе ulog и zerolog.
//   - Проверку доступности СУБД PostgreSQL и автоматический запуск миграций.
//   - Сборку слоев Чистой Архитектуры (Clean Architecture) через Dependency Injection.
//   - Инициализацию фонового асинхронного воркера обработки начислений.
//   - Конфигурирование сетевого периметра HTTP-сервера и роутера.
//   - Реализацию потокобезопасного механизма деликатного завершения работы (Graceful Shutdown).
package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ioncode/gofermart/internal/config"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/worker"
	"github.com/ioncode/ulog/v3"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

// main инициализирует и координирует жизненный цикл всего приложения.
func main() {
	// Настраиваем глобальное поведение сериализации библиотеки decimal.
	// Флаг True заставляет кодировщик выводить копейки баллов как чистые числа в JSON,
	// избавляя клиентские приложения от необходимости парсить строки в кавычках.
	decimal.MarshalJSONWithoutQuotes = true

	// Инициализация слоя структурированного логирования для отслеживания инцидентов.
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}
	logWriter := consoleWriter
	nativeZerolog := zerolog.New(logWriter).With().Timestamp().Caller().Logger()
	logger := uzerolog.NewZerologAdapter(nativeZerolog)

	// Загрузка конфигурационных параметров из флагов CLI и системного окружения (ENV).
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Критическая ошибка конфигурации приложения", err)
		os.Exit(1)
	}
	logger.Info("Конфигурация успешно загружена", ulog.String("addr", cfg.RunAddress))

	// Запуск автоматического наката схем таблиц базы данных до открытия основного пула.
	if err := postgres.RunMigrations(cfg.DatabaseURI); err != nil {
		logger.Error("Критическая ошибка применения SQL-миграций", err)
		os.Exit(1)
	}
	logger.Info("Миграции базы данных успешно применены")

	// Инициализация пула долгоживущих соединений к PostgreSQL с поддержкой Decimal.
	poolCtx, poolCancel := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := postgres.NewPoolWithDecimal(poolCtx, cfg.DatabaseURI)
	poolCancel()
	if err != nil {
		logger.Error("Не удалось инициализировать пул подключений к БД", err)
		os.Exit(1)
	}
	logger.Info("Успешное подключение к СУБД PostgreSQL зафиксировано")
	defer pool.Close()

	// Сборка репозиториев инфраструктурного слоя хранения данных.
	userRepo := postgres.NewUserRepository(pool)
	orderRepo := postgres.NewOrderRepository(pool)
	balanceRepo := postgres.NewBalanceRepository(pool)
	accrualRepo := postgres.NewOrderAccrualRepository(pool)

	// Инициализация и запуск фонового распределителя задач обработки заказов.
	accrualWorker := worker.NewAccrualWorker(orderRepo, accrualRepo, cfg.AccrualSystemAddress, logger)

	// Инициализация доменного сервисного слоя бизнес-логики приложения.
	loyaltySvc := service.NewLoyaltyService(
		userRepo,
		orderRepo,
		balanceRepo,
		cfg.JWTSecret,
		cfg.TokenTTL,
		logger,
		accrualWorker.OrderChan,
	)

	// Инициализация хендлера и сборка сетевого роутера.
	// Передаем userHandler, который удовлетворяет интерфейсу router.ServerHandler.
	userHandler := handler.NewUserHandler(loyaltySvc, false, logger)
	server := router.NewServer(cfg.RunAddress, userHandler, logger)

	// Асинхронный запуск прослушивания HTTP-порта в выделенной горутине.
	go server.Start()

	// Перехват системных сигналов операционной системы для организации плавного закрытия.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Запуск фонового планировщика подстраховки воркера с циклом опроса в 5 секунд.
	// для эффективного использования ресурсов обработчики канала вызываются асинхронно в отдельных горутинах

	// создаем группу ожидания для контроля горутин воркера
	var wg sync.WaitGroup

	// добавляем счетчик горутины самого старта
	wg.Add(1)

	go func() {
		defer wg.Done() // Этот Done сработает, когда сам цикл Start сделает return
		accrualWorker.Start(rootCtx, 5*time.Second, &wg)
	}()

	// Блокировка основного потока приложения до получения сигнала SIGTERM/SIGINT.
	<-rootCtx.Done()
	logger.Info("Получен системный сигнал завершения. Запускается Graceful Shutdown...")

	// Деликатная остановка веб-сервера с жестким таймаутом ожидания активных запросов.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("Ошибка при остановке HTTP-сервера", err)
		os.Exit(1)
	}

	logger.Info("HTTP-сервер успешно остановлен. Новые запросы больше не поступают.")

	// Гарантированно закрываем канал передачи событий, останавливая входящий поток задач.
	close(accrualWorker.OrderChan)
	logger.Debug("Входящий канал фонового воркера успешно заблокирован")

	logger.Info("Ожидание завершения работы всех фоновых горутин воркера...")
	wg.Wait()

	logger.Info("Все системные ресурсы освобождены. Приложение успешно остановлено.")
}
