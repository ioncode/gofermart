package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"time"

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
	// =========================================================================
	// 1. ВРЕМЕННОЕ ПОДКЛЮЧЕНИЕ ДЛЯ ПРОВЕРКИ РЕКОМЕНДАТЕЛЬНОГО ЗАМКА
	// =========================================================================
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open tech connection for lock check: %w", err)
	}
	defer db.Close()

	// Выполняем неблокирующую попытку захвата замка с ID 42 на уровне СУБД.
	// Метод мгновенно возвращает true (свободен/захвачен) или false (занят другим подом).
	var acquired bool
	err = db.QueryRow(`SELECT pg_try_advisory_lock(1777)`).Scan(&acquired)
	if err != nil {
		return fmt.Errorf("failed to check postgres advisory lock: %w", err)
	}

	// Если замок занят, значит, параллельный под УЖЕ накатывает миграции в этот миг.
	// Этот под просто пропускает шаг и выходит с успехом, чтобы main.go сразу запускал HTTP-сервер.
	if !acquired {
		// Опционально: делаем микро-паузу, чтобы ведущий под успел физически
		// завершить DDL-запросы до того, как мы начнем отвечать клиентам по API.
		time.Sleep(1 * time.Second)
		return nil
	}

	// Гарантированно освобождаем замок на уровне сессии PostgreSQL,
	// как только текущий (ведущий) под завершит функцию RunMigrations.
	defer func() {
		_, _ = db.Exec(`SELECT pg_advisory_unlock(1777)`)
	}()
	// =========================================================================

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

	config.MaxConns = 50                      // Максимальное количество одновременных активных соединений с СУБД
	config.MinConns = 10                      // Минимальное число удерживаемых «горячих» коннектов в пуле (убирает задержки на старте)
	config.MaxConnIdleTime = 15 * time.Minute // Время жизни неиспользуемого соединения перед его деликатным закрытием
	config.MaxConnLifetime = 1 * time.Hour    // Абсолютное время жизни коннекта для предотвращения утечек памяти на стороне Postgres

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
