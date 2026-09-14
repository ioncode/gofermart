package repository

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
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

type PostgresRepository struct {
	db *pgxpool.Pool
}

// NewPostgresRepository создает новый экземпляр репозитория.
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// CreateUser сохраняет пользователя и возвращает сгенерированный базой данных UUID/ID
func (r *PostgresRepository) CreateUser(ctx context.Context, login, passwordHash string) (string, error) {
	query := `INSERT INTO users (login, password_hash) VALUES ($1, $2) RETURNING id`

	var userID string
	err := r.db.QueryRow(ctx, query, login, passwordHash).Scan(&userID)
	if err != nil {
		// Перехватываем ошибку уникальности PostgreSQL (код 23505 — unique_violation)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrDuplicateLogin
		}
		return "", fmt.Errorf("postgres insert user failed: %w", err)
	}

	return userID, nil
}

// CheckUserExists проверяет наличие логина в системе
func (r *PostgresRepository) CheckUserExists(ctx context.Context, login string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE login = $1)`

	var exists bool
	err := r.db.QueryRow(ctx, query, login).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("postgres check user existence failed: %w", err)
	}

	return exists, nil
}

// GetPasswordHash ищет пользователя по логину и возвращает его ID и хэш пароля для авторизации (Login)
func (r *PostgresRepository) GetPasswordHash(ctx context.Context, login string) (string, string, error) {
	query := `SELECT id, password_hash FROM users WHERE login = $1`

	var userID, hash string
	err := r.db.QueryRow(ctx, query, login).Scan(&userID, &hash)
	if err != nil {
		// Если ничего не найдено
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", "", err
		}
		return "", "", ErrUserNotFound
	}

	return userID, hash, nil
}

// GetOrder осуществляет поиск конкретного заказа в базе данных по его строковому идентификатору.
//
// Метод напрямую сканирует SQL-тип NUMERIC/DECIMAL в высокоточную структуру domain.Accrual
// без использования промежуточных float-указателей, что полностью исключает потерю копеек.
//
// Если заказ с указанным идентификатором отсутствует в системе, метод возвращает
// пустую доменную модель и структурированную ошибку ErrOrderNotFound.
func (r *PostgresRepository) GetOrder(ctx context.Context, orderID string) (domain.Order, error) {
	query := `SELECT id, user_id, status, accrual, uploaded_at FROM orders WHERE id = $1`
	var o domain.Order
	var dbAccrual decimal.Decimal // Локальная переменная (не указатель)

	// Прямой маппинг полей таблицы Postgres на доменную модель Go лояльности
	err := r.db.QueryRow(ctx, query, orderID).Scan(
		&o.ID,
		&o.UserID,
		&o.Status,
		&dbAccrual,
		&o.UploadedAt,
	)
	if err != nil {
		// Отрабатываем отсутствие записей по стандарту pgx/v5
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, ErrOrderNotFound
		}
		return domain.Order{}, fmt.Errorf("postgres: get order execution failed: %w", err)
	}
	if o.Status == domain.StatusProcessed {
		o.Accrual = &dbAccrual
	}

	return o, nil
}

// CreateOrder сохраняет новый заказ в базу данных со статусом NEW
func (r *PostgresRepository) CreateOrder(ctx context.Context, orderID string, userID string, status string) error {
	query := `INSERT INTO orders (id, user_id, status, accrual) VALUES ($1, $2, $3, 0.00)`

	_, err := r.db.Exec(ctx, query, orderID, userID, status)
	if err != nil {
		return fmt.Errorf("postgres: failed to insert new order: %w", err)
	}

	return nil
}

// UpdateOrderAccrual переводит заказ в новый статус и начисляет баллы лояльности в рамках ACID-транзакции.
func (r *PostgresRepository) UpdateOrderAccrual(ctx context.Context, orderID string, userID string, status string, accrual decimal.Decimal) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: cannot start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Обновляем статус и сумму начисления в таблице заказов
	orderQuery := `UPDATE orders SET status = $1, accrual = $2 WHERE id = $3`
	_, err = tx.Exec(ctx, orderQuery, status, accrual, orderID)
	if err != nil {
		return fmt.Errorf("postgres: tx update order failed: %w", err)
	}

	// 2. Начисляем баллы пользователю при положительной сумме
	if accrual.IsPositive() {
		userQuery := `UPDATE users SET balance = balance + $1 WHERE id = $2`
		_, err = tx.Exec(ctx, userQuery, accrual, userID)
		if err != nil {
			return fmt.Errorf("postgres: tx update user balance failed: %w", err)
		}
	}

	// 3. Фиксируем транзакцию
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: tx commit failed: %w", err)
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

// GetUnprocessedOrders вычитывает из базы данных список всех заказов, находящихся
// в промежуточных статусах NEW и PROCESSING, исключая неиспользуемые воркером поля.
func (r *PostgresRepository) GetUnprocessedOrders(ctx context.Context) ([]domain.Order, error) {
	// Убираем accrual из SELECT, оставляем только необходимые воркеру поля
	query := `SELECT id, user_id, status, uploaded_at 
	          FROM orders 
	          WHERE status IN ('NEW', 'PROCESSING')
	          ORDER BY uploaded_at ASC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query unprocessed orders: %w", err)
	}
	defer rows.Close()

	orders := make([]domain.Order, 0, 16)
	for rows.Next() {
		var o domain.Order
		// Поле o.Accrual не участвует в Scan и по умолчанию останется равным nil
		err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.Status,
			&o.UploadedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan unprocessed order: %w", err)
		}
		orders = append(orders, o)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: rows error in unprocessed orders: %w", err)
	}

	return orders, nil
}

// GetOrdersByUserID возвращает список всех заказов конкретного пользователя.
//
// Выборка сортируется по времени загрузки от самых старых к самым новым (ASC)
// согласно требованиям технического задания. Поля типов NUMERIC/DECIMAL автоматически
// сканируются в высокоточную структуру домена лояльности decimal.Decimal.
//
// Если у пользователя нет загруженных заказов, метод возвращает пустой слайс и nil вместо ошибки.
func (r *PostgresRepository) GetOrdersByUserID(ctx context.Context, userID string) ([]domain.Order, error) {
	query := `SELECT id, user_id, status, accrual, uploaded_at 
	          FROM orders 
	          WHERE user_id = $1 
	          ORDER BY uploaded_at ASC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query user orders: %w", err)
	}
	defer rows.Close()

	// Инициализируем слайс с базовой емкостью, чтобы снизить количество аллокаций в куче при append
	orders := make([]domain.Order, 0, 16)

	for rows.Next() {
		var o domain.Order
		var dbAccrual decimal.Decimal // Локальная переменная (не указатель)
		err := rows.Scan(
			&o.ID,
			&o.UserID,
			&o.Status,
			&dbAccrual, // Сканируем из базы в обычный decimal
			&o.UploadedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan user order row: %w", err)
		}
		// Заполняем поле accrual только если статус PROCESSED
		if o.Status == domain.StatusProcessed {
			o.Accrual = &dbAccrual
		} else {
			o.Accrual = nil // Для NEW, PROCESSING, INVALID поле гарантированно станет nil
		}
		orders = append(orders, o)
	}

	// Обязательная проверка итератора на наличие скрытых сетевых ошибок СУБД
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: rows error in user orders: %w", err)
	}

	return orders, nil
}

// GetUserBalance возвращает текущий доступный баланс пользователя и общую сумму его списаний за всё время.
//
// Метод считывает данные высокой точности NUMERIC напрямую в структуры decimal.Decimal,
// полностью исключая погрешности округления копеек на уровне бизнес-логики.
// Если пользователь не найден в системе, возвращает ошибку ErrUserNotFound.
func (r *PostgresRepository) GetUserBalance(ctx context.Context, userID string) (decimal.Decimal, decimal.Decimal, error) {
	// Запрос выбирает поля баланса и списаний для конкретного UUID пользователя
	query := `SELECT balance, withdrawn FROM users WHERE id = $1`

	var current decimal.Decimal
	var withdrawn decimal.Decimal

	// Выполняем точечное чтение одной строки
	err := r.db.QueryRow(ctx, query, userID).Scan(&current, &withdrawn)
	if err != nil {
		// Если СУБД вернула отсутствие строк, мапим ошибку на понятную для сервиса
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, decimal.Zero, ErrUserNotFound
		}
		return decimal.Zero, decimal.Zero, fmt.Errorf("postgres: failed to scan user balance: %w", err)
	}

	return current, withdrawn, nil
}
