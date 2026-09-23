package accrual

import (
	"context"

	"fmt"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

type Service struct {
	orderRepo   repository.OrderRepository
	accrualRepo repository.OrderAccrualRepository
	client      *Client
	throttler   *Throttler
	logger      ulog.Logger
	processing  sync.Map
}

// GetUnprocessedOrders запрашивает незавершенные заказы из репозитория
func (s *Service) GetUnprocessedOrders(ctx context.Context) ([]domain.Order, error) {
	return s.orderRepo.GetUnprocessedOrders(ctx)
}

// ProcessOrder координирует весь цикл обработки одного заказа
func (s *Service) ProcessOrder(ctx context.Context, order domain.Order, source string) {
	orderLogger := s.logger.With(ulog.String("order_id", order.ID), ulog.String("source", source))

	defer func() {
		if r := recover(); r != nil {
			orderLogger.Error(fmt.Sprintf("КРИТИЧЕСКАЯ ПАНИКА при обработке заказа: %v", r), nil)
			s.processing.Delete(order.ID)
		}
	}()

	// Защита от параллельного дублирования обработки по order.ID
	if _, loaded := s.processing.LoadOrStore(order.ID, true); loaded {
		return
	}
	defer s.processing.Delete(order.ID)

	// Выполнение сетевого запроса к внешней системе начислений
	res, err := s.client.CheckOrder(ctx, order.ID)
	if err != nil {
		orderLogger.Error("Ошибка сетевого запроса к accrual", err)
		return
	}

	if res.StatusCode == http.StatusTooManyRequests {
		orderLogger.Debug("Получен HTTP 429. Активируется экстренная блокировка", ulog.Int("backoff_sec", int(res.Backoff.Seconds())))
		s.throttler.Block(res.Backoff)
		return
	}

	if res.StatusCode == http.StatusNoContent {
		orderLogger.Debug("Заказ еще не зарегистрирован в системе accrual (204)")
		return
	}

	if res.StatusCode != http.StatusOK {
		orderLogger.Error(fmt.Sprintf("Неожиданный статус от accrual: %d", res.StatusCode), nil)
		return
	}

	var accResp AccrualResponse
	if err := json.Unmarshal(res.Body, &accResp); err != nil {
		orderLogger.Error("Ошибка парсинга JSON ответа", err)
		return
	}

	// Синхронизируем состояние нашего домена на основе внешних данных
	s.syncOrderStatus(ctx, order, accResp, orderLogger)
}

// syncOrderStatus переводит заказ в актуальный статус на основе ответа системы начислений
func (s *Service) syncOrderStatus(ctx context.Context, order domain.Order, accResp AccrualResponse, logger ulog.Logger) {
	switch accResp.Status {

	case domain.AccrualProcessed:
		var val decimal.Decimal
		if accResp.Accrual != nil {
			val = *accResp.Accrual
		}
		// 🟢 Обрабатываем ошибку транзакции обновления баланса
		err := s.accrualRepo.UpdateOrderAndBalance(ctx, order.ID, order.UserID, string(domain.StatusProcessed), val)
		if err != nil {
			logger.Error("Не удалось обновить баланс и закрыть заказ в БД", err)
			return // Прерываемся, логи о завершении ниже не выполнятся
		}
		logger.Info("Заказ успешно обработан (PROCESSED), баллы начислены")

	case domain.AccrualInvalid:
		// 🟢 Обрабатываем ошибку фиксации невалидного заказа
		err := s.accrualRepo.UpdateOrderAndBalance(ctx, order.ID, order.UserID, string(domain.StatusInvalid), decimal.NewFromInt(0))
		if err != nil {
			logger.Error("Не удалось перевести заказ в статус INVALID в БД", err)
			return
		}
		logger.Debug("Заказ признан невалидным (INVALID) внешней системой")

	case domain.AccrualProcessing:
		if order.Status == domain.StatusNew {
			// 🟢 Обрабатываем ошибку перевода заказа во временный статус обработки
			err := s.accrualRepo.UpdateOrderAndBalance(ctx, order.ID, order.UserID, string(domain.StatusProcessing), decimal.NewFromInt(0))
			if err != nil {
				logger.Error("Не удалось обновить статус заказа на PROCESSING в БД", err)
				return
			}
			logger.Debug("Заказ переведен во внутренний статус PROCESSING")
		}

	case domain.AccrualRegistered:
		logger.Debug("Заказ зарегистрирован во внешней системе (REGISTERED), ожидаем расчета")
	}
}
