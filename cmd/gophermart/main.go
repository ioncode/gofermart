package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ioncode/gofermart/internal/config"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/ioncode/gofermart/internal/worker"
	"github.com/ioncode/ulog/v3"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
)

func main() {
	// 1. Настройка логгера ulog + zerolog

	// локально используем консольный вывод красивых логов

	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339, // Красивый читаемый формат времени (например, 2026-09-11T16:15:00Z)
	}
	// на проде logWriter = os.Stdout
	logWriter := consoleWriter
	nativeZerolog := zerolog.New(logWriter).With().Timestamp().Caller().Logger()
	logger := uzerolog.NewZerologAdapter(nativeZerolog)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("Критическая ошибка конфигурации", err)
		os.Exit(1)
	}

	logger.Info("Конфигурация успешно загружена", ulog.String("addr", cfg.RunAddress))

	// 2. Выполнение миграций базы данных до инициализации основного пула соединений.
	// Если таблицы не созданы или СУБД недоступна, приложение упадет здесь.
	if err := repository.RunMigrations(cfg.DatabaseURI); err != nil {
		logger.Error("Критическая ошибка применения миграций", err)
		os.Exit(1)
	} else {
		logger.Info("Миграции успешно применены")
	}

	// 3. Инициализация пула соединений с PostgreSQL.
	// Контекст отменяется сразу после успешного (или неуспешного) подключения.
	poolCtx, poolCancel := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := repository.NewPoolWithDecimal(poolCtx, cfg.DatabaseURI)
	poolCancel()
	if err != nil {
		logger.Error("Не удалось подключиться к базе данных", err)
		os.Exit(1)
	} else {
		logger.Info("Успешно подключились к БД")
	}

	// Дефер гарантирует закрытие пула соединений при завершении функции main().
	defer pool.Close()

	// 4. Сборка слоев приложения согласно Чистой Архитектуре (Dependency Injection).
	repo := repository.NewPostgresRepository(pool)
	// Инициализируем фоновый воркер
	accrualWorker := worker.NewAccrualWorker(repo, cfg.AccrualSystemAddress, logger)
	loyaltySvc := service.NewLoyaltyService(repo, cfg.JWTSecret, cfg.TokenTTL, logger, accrualWorker.OrderChan)
	userHandler := handler.NewUserHandler(loyaltySvc, false)
	server := router.NewServer(cfg.RunAddress, userHandler, logger)

	// 5. Старт HTTP-сервера в отдельной горутине, чтобы не блокировать основной поток.
	go server.Start()

	// 6. Реализация идиоматичного Graceful Shutdown с помощью NotifyContext.
	// Слушаем сигналы завершения работы (Ctrl+C или остановка контейнера).
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 7. Запускаем воркер в фоне. Плановый тикер подстраховки настраиваем на 5 секунд.
	go accrualWorker.Start(rootCtx, 5*time.Second)

	// Блокируемся и ждем системного сигнала.
	<-rootCtx.Done()
	logger.Info("Получен сигнал завершения работы ОС. Начинаем плавную остановку сервера...")

	// 8. Закрываем канал для записи новых заказов (хендлеры перестанут слать новые события)
	close(accrualWorker.OrderChan)
	logger.Debug("Канал воркера успешно закрыт для записи")

	// 9. Ограничиваем время ожидания завершения текущих HTTP-запросов до 5 секунд.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("Ошибка при плавном завершении работы сервера", err)
		os.Exit(1)
	}

	logger.Info("Приложение успешно и безопасно остановлено.")
}
