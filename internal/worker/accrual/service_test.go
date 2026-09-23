package accrual

import (
	"context"
	"errors"
	"testing"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository/mocks"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
)

func TestService_SyncOrderStatus_Success(t *testing.T) {
	// 1. Инициализируем контроллер GoMock
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 2. Создаем мок репозитория баланса и заказов
	mockAccrualRepo := mocks.NewMockOrderAccrualRepository(ctrl)

	// Выключаем реальный вывод логов в консоль на время теста
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))

	// Инициализируем сервис, подкладывая только нужный мок
	s := &Service{
		accrualRepo: mockAccrualRepo,
		logger:      logger,
	}

	ctx := context.Background()

	// Исходная доменная модель заказа в статусе NEW
	order := domain.Order{
		ID:     "12345678903",
		UserID: "user-uuid-123",
		Status: domain.StatusNew,
	}

	accrualPoints := decimal.NewFromFloat(500.50)

	// Ответ от внешней системы accrual
	accResp := AccrualResponse{
		Order:   "12345678903",
		Status:  domain.AccrualProcessed, // Финальный статус начисления
		Accrual: &accrualPoints,
	}

	// 3. Настраиваем ожидания (Expectations) для мока:
	// Мы ожидаем, что метод UpdateOrderAndBalance вызовется строго 1 раз
	// с конкретными строковыми параметрами "PROCESSED" и суммой 500.50, и вернет nil (успех).
	mockAccrualRepo.EXPECT().
		UpdateOrderAndBalance(
			ctx,
			order.ID,
			order.UserID,
			string(domain.StatusProcessed),
			accrualPoints,
		).
		Return(nil).
		Times(1)

	// 4. Выполняем тестируемый метод напрямую
	s.syncOrderStatus(ctx, order, accResp, logger)
}

func TestService_SyncOrderStatus_DatabaseError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAccrualRepo := mocks.NewMockOrderAccrualRepository(ctrl)
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil))

	s := &Service{
		accrualRepo: mockAccrualRepo,
		logger:      logger,
	}

	ctx := context.Background()
	order := domain.Order{
		ID:     "79927398713",
		UserID: "user-uuid-999",
		Status: domain.StatusNew,
	}

	accResp := AccrualResponse{
		Order:  "79927398713",
		Status: domain.AccrualInvalid, // Сервис вернул INVALID
	}

	// Имитируем падение базы данных (например, дедлок или обрыв соединения)
	dbErr := errors.New("postgres: connection refused or transaction deadlock")

	// Настраиваем ожидания: метод UpdateOrderAndBalance возвращает ошибку
	mockAccrualRepo.EXPECT().
		UpdateOrderAndBalance(
			ctx,
			order.ID,
			order.UserID,
			string(domain.StatusInvalid),
			decimal.NewFromInt(0),
		).
		Return(dbErr).
		Times(1)

	// Вызываем метод. Благодаря написанному ранее коду, метод обработает ошибку,
	// запишет её в логгер и сделает досрочный return, защитив приложение от ложных логов успеха.
	s.syncOrderStatus(ctx, order, accResp, logger)
}
