package postgres_test

import (
	"context"
	"time"

	pgrepo "github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// BasePostgresTestSuite инкапсулирует общую логику запуска Docker контейнера
type BasePostgresTestSuite struct {
	suite.Suite
	pgContainer *tcpostgres.PostgresContainer
	Pool        *pgxpool.Pool
}

// SetupSuite запускает один инстанс PostgreSQL и накатывает embed-миграции.
// Этот метод автоматически наследуется всеми сьютами, которые используют BasePostgresTestSuite.
func (s *BasePostgresTestSuite) SetupSuite() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 1. Запускаем изолированный контейнер Postgres в Docker
	pgCtr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("gofermart_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
	)
	s.Require().NoError(err)
	s.pgContainer = pgCtr

	connStr, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	s.Require().NoError(err)

	// 2. Механизм Retry против EOF сбоев сетевого стека Docker на Windows
	var pool *pgxpool.Pool
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.New(ctx, connStr)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			}
			pool.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.Require().NoError(err, "PostgreSQL failed to become ready for connections")
	s.Pool = pool

	// 3. Автоматически накатываем встроенные боевые embed-миграции
	err = pgrepo.RunMigrations(connStr)
	s.Require().NoError(err)
}

// TearDownTest гарантирует чистоту данных: очищает таблицы перед каждым тест-кейсом
// очищаем руками только пользователей, остальные таблицы очистятся каскадом
func (s *BasePostgresTestSuite) TearDownTest() {
	_, err := s.Pool.Exec(context.Background(), "TRUNCATE users CASCADE")
	s.Require().NoError(err)
}

// TearDownSuite тушит Docker-контейнер после завершения всех тестов конкретного сьюта
func (s *BasePostgresTestSuite) TearDownSuite() {
	ctx := context.Background()
	if s.Pool != nil {
		s.Pool.Close()
	}
	if s.pgContainer != nil {
		_ = s.pgContainer.Terminate(ctx)
	}
}
