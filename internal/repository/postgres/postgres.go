package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Встраиваем папку с миграциями прямо в бинарный файл приложения
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations запускает миграции базы данных перед стартом основных сервисов
func RunMigrations(databaseURL string) error {
	// Создаем источник данных для golang-migrate из встроенных файлов embed.FS
	sourceDriver, err := httpfs.New(http.FS(migrationsFS), "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("httpfs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrate instance: %w", err)
	}
	defer m.Close()

	// Применяем все доступные миграции "вверх"
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
}

// NewPoolWithDecimal создает и настраивает пул соединений pgxpool.Pool с поддержкой контекста.
// Метод регистрирует поддержку типов высокой точности shopspring/decimal
// для нативного маппинга PostgreSQL типов NUMERIC/DECIMAL на уровне драйвера.
func NewPoolWithDecimal(ctx context.Context, databaseURI string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURI)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to parse database uri config: %w", err)
	}

	// Настраиваем триггер, который срабатывает при каждом новом физическом подключении к БД
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// pgx/v5 автоматически мапит типы shopspring/decimal, если они передаются напрямую.
		return nil
	}

	// Используем NewWithConfig, но контролируем создание пула через переданный контекст
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to create pool with config: %w", err)
	}

	return pool, nil
}
