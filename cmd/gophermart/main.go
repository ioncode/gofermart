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
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ioncode/gofermart/internal/config"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/worker/accrual"
	"github.com/ioncode/ulog/v3"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// main инициализирует и координирует жизненный цикл всего приложения.
func main() {
	// Настраиваем глобальное поведение сериализации библиотеки decimal.
	// Флаг True заставляет кодировщик выводить копейки баллов как чистые числа в JSON,
	// избавляя клиентские приложения от необходимости парсить строки в кавычках.
	decimal.MarshalJSONWithoutQuotes = true

	const RFC3339Milli = "2006-01-02T15:04:05.000Z07:00"

	// Инициализация слоя структурированного логирования для отслеживания инцидентов.
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: RFC3339Milli,
	}
	zerolog.TimeFieldFormat = RFC3339Milli
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
	accrualWorker := accrual.NewAccrualWorker(orderRepo, accrualRepo, cfg.AccrualSystemAddress, logger)

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

	// Перехват системных сигналов операционной системы для организации плавного закрытия.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Инициализируем errgroup для управления жизненным циклом фоновых процессов
	eg, groupCtx := errgroup.WithContext(rootCtx)

	eg.Go(func() error {
		return server.Start()
	})

	eg.Go(func() error {
		// Запуск тикера с интервалом подстраховки в 5 секунд
		if err := accrualWorker.Start(groupCtx, 5*time.Second); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Критическая ошибка воркера начислений", err)
			return err
		}
		return nil
	})

	// Ожидаем прерывания (системный сигнал Ctrl+C или ошибка в любой из горутин errgroup)
	<-groupCtx.Done()
	logger.Info("Получен сигнал завершения работы. Инициализация Graceful Shutdown...")

	// ПОСЛЕДОВАТЕЛЬНОСТЬ GRACEFUL SHUTDOWN

	// Шаг А: Мгновенно останавливаем прием новых HTTP-запросов (таймаут 5 сек на закрытие текущих)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("Ошибка при плановой остановке HTTP-сервера", err)
	} else {
		logger.Info("HTTP-сервер успешно остановлен, новые запросы не принимаются")
	}

	// Шаг Б: Безопасно закрываем входящий канал воркера.
	// Хендлеры веб-сервера больше ничего туда не пишут, так как сервер уже закрыт.
	close(accrualWorker.OrderChan)
	logger.Info("Канал OrderChan закрыт. Ожидаем вычитки оставшихся в буфере задач...")

	// Шаг В: Блокируем главный поток и ждем, пока воркер дочитает канал до дна
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Приложение завершилось с ошибкой", err)
	} else {
		logger.Info("Все фоновые процессы успешно завершили работу. Данные консистентны.")
	}
}
