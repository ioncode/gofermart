package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ioncode/gofermart/internal/handler"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/gofermart/internal/router"
	"github.com/ioncode/gofermart/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	jwtSecret := "super-secret-key-change-me-in-production"
	tokenTTL := 24 * time.Hour
	dbURL := "postgres://myuser:mysecretpassword@localhost:5433/mydatabase?sslmode=disable"
	if err := repository.RunMigrations(dbURL); err != nil {
		log.Fatalf("Критическая ошибка применения миграций: %v", err)
	}

	// 1. Инициализируем пул базы данных
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	log.Println(pool)
	// Важно: закрываем пул при выходе из main, это гарантирует отпускание всех коннекшенов к БД
	defer pool.Close()

	// 2. Инициализируем слои по цепочке: Repository -> Service -> Handler -> Router
	repo := repository.NewPostgresRepository(pool)
	loyaltySvc := service.NewLoyaltyService(repo, jwtSecret, tokenTTL)
	userHandler := handler.NewUserHandler(loyaltySvc)
	server := router.NewServer("8080", userHandler)

	// Запуск сервера в фоновой горутине
	go server.Start()

	// 1. Создаем родительский контекст, который отменится при сигналах SIGINT или SIGTERM
	// Функция stop() освобождает ресурсы, связанные с перехватом сигналов
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 2. Блокируемся и ждем отмены контекста (когда прилетит сигнал ОС)
	<-rootCtx.Done()
	log.Println("[Main] Получен сигнал завершения. Начинаем Graceful Shutdown...")

	// 3. Создаем контекст с таймаутом на 5 секунд исключительно для завершения активных HTTP-запросов
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Fatalf("[Main] Ошибка при плавном завершении сервера: %v", err)
	}

	log.Println("[Main] Приложение успешно остановлено.")
}
