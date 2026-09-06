package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ioncode/gofermart/internal/config"
	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// 1. Загрузка конфигурации.
	// Вся магия с confy, флагами и переменными окружения инкапсулирована внутри этого вызова.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[Main] Критическая ошибка конфигурации приложения: %v", err)
	}

	log.Printf("[Main] Конфигурация успешно загружена. Сервер запустится на: %s", cfg.RunAddress)

	// 2. Выполнение миграций базы данных до инициализации основного пула соединений.
	// Если таблицы не созданы или СУБД недоступна, приложение упадет здесь.
	if err := repository.RunMigrations(cfg.DatabaseURI); err != nil {
		log.Fatalf("[Main] Критическая ошибка применения миграций: %v", err)
	}

	// 3. Инициализация пула соединений с PostgreSQL.
	// Контекст отменяется сразу после успешного (или неуспешного) подключения.
	poolCtx, poolCancel := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := pgxpool.New(poolCtx, cfg.DatabaseURI)
	poolCancel()
	if err != nil {
		log.Fatalf("[Main] Не удалось подключиться к базе данных: %v", err)
	}
	// Дефер гарантирует закрытие пула соединений при завершении функции main().
	defer pool.Close()

	// 4. Сборка слоев приложения согласно Чистой Архитектуре (Dependency Injection).
	repo := repository.NewPostgresRepository(pool)
	loyaltySvc := service.NewLoyaltyService(repo, cfg.JWTSecret, cfg.TokenTTL)
	userHandler := handler.NewUserHandler(loyaltySvc)
	server := router.NewServer(cfg.RunAddress, userHandler)

	// 5. Старт HTTP-сервера в отдельной горутине, чтобы не блокировать основной поток.
	go server.Start()

	// 6. Реализация идиоматичного Graceful Shutdown с помощью NotifyContext.
	// Слушаем сигналы завершения работы (Ctrl+C или остановка контейнера).
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Блокируемся и ждем системного сигнала.
	<-rootCtx.Done()
	log.Println("[Main] Получен сигнал завершения работы ОС. Начинаем плавную остановку сервера...")

	// 7. Ограничиваем время ожидания завершения текущих HTTP-запросов до 5 секунд.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Fatalf("[Main] Ошибка при плавном завершении работы сервера: %v", err)
	}

	log.Println("[Main] Приложение успешно и безопасно остановлено.")
}
