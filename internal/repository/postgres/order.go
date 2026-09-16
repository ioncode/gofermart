package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type OrderRepository struct {
	db *pgxpool.Pool
}

// UserRepository создает новый экземпляр репозитория для работы с таблицей пользователей
// Пул pgxpool.Pool должен инициализироваться в main.go и передаваться сюда.
func NewOrderRepository(db *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{db: db}
}

// GetOrder осуществляет поиск конкретного заказа в базе данных по его строковому идентификатору.
//
// Метод напрямую сканирует SQL-тип NUMERIC/DECIMAL в высокоточную структуру domain.Accrual
// без использования промежуточных float-указателей, что полностью исключает потерю копеек.
//
// Если заказ с указанным идентификатором отсутствует в системе, метод возвращает
// пустую доменную модель и структурированную ошибку ErrOrderNotFound.
func (r *OrderRepository) GetOrder(ctx context.Context, orderID string) (domain.Order, error) {
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
			return domain.Order{}, repository.ErrOrderNotFound
		}
		return domain.Order{}, fmt.Errorf("postgres: get order execution failed: %w", err)
	}
	if o.Status == domain.StatusProcessed {
		o.Accrual = &dbAccrual
	}

	return o, nil
}

// CreateOrder сохраняет новый заказ в базу данных со статусом NEW
func (r *OrderRepository) CreateOrder(ctx context.Context, orderID string, userID string, status string) error {
	query := `INSERT INTO orders (id, user_id, status, accrual) VALUES ($1, $2, $3, 0.00)`

	_, err := r.db.Exec(ctx, query, orderID, userID, status)
	if err != nil {
		return fmt.Errorf("postgres: failed to insert new order: %w", err)
	}

	return nil
}

// GetUnprocessedOrders вычитывает из базы данных список всех заказов, находящихся
// в промежуточных статусах NEW и PROCESSING, исключая неиспользуемые воркером поля.
func (r *OrderRepository) GetUnprocessedOrders(ctx context.Context) ([]domain.Order, error) {
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
// Выборка сортируется по времени загрузки от самых новых к самым старым (DESC)
// согласно требованиям технического задания. Поля типов NUMERIC/DECIMAL автоматически
// сканируются в высокоточную структуру домена лояльности decimal.Decimal.
//
// Если у пользователя нет загруженных заказов, метод возвращает пустой слайс и nil вместо ошибки.
func (r *OrderRepository) GetOrdersByUserID(ctx context.Context, userID string) ([]domain.Order, error) {
	query := `SELECT id, user_id, status, accrual, uploaded_at 
	          FROM orders 
	          WHERE user_id = $1 
	          ORDER BY uploaded_at DESC`

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
